/**
 * @file Touch-friendly console accessory bar for the SSH/Telnet terminal,
 * in the spirit of Termius / Blink on phones.
 *
 * A phone's software keyboard has no Esc, Tab, Ctrl, Alt, arrows or
 * function keys, and by default it slides up *over* the terminal. This
 * module fixes both:
 *
 *   - A scrollable toolbar sits directly above the OS keyboard with the
 *     missing keys. Ctrl / Alt / Shift are sticky one-shot modifiers
 *     (tap to arm, they light up, apply to the next key, then release;
 *     tap again to disarm). Modifiers combine with the OS keyboard too:
 *     arm Ctrl, then press "c" on the phone keyboard, and it is sent as
 *     ^C.
 *   - The terminal is kept fully visible above the keyboard using the
 *     visualViewport API, so nothing is ever hidden behind it.
 *
 * The module is inert on non-touch devices (returns an identity
 * transform), so desktop behaviour is untouched.
 */

import { t } from './i18n.js'

// isTouchDevice reports whether this looks like a phone / tablet: a
// coarse primary pointer. Desktop with a mouse returns false and the
// whole module no-ops.
export function isTouchDevice() {
  try {
    return (
      (window.matchMedia && window.matchMedia('(pointer: coarse)').matches) ||
      'ontouchstart' in window ||
      (navigator.maxTouchPoints || 0) > 0
    )
  } catch {
    return false
  }
}

// ctrlByte maps a printable character to its control code (Ctrl+key).
function ctrlByte(ch) {
  const c = ch.toLowerCase()
  if (c >= 'a' && c <= 'z') return String.fromCharCode(c.charCodeAt(0) - 96)
  const map = {
    ' ': '\x00',
    '@': '\x00',
    '[': '\x1b',
    '\\': '\x1c',
    ']': '\x1d',
    '^': '\x1e',
    '_': '\x1f',
    '?': '\x7f',
    '2': '\x00',
    '3': '\x1b',
    '4': '\x1c',
    '5': '\x1d',
    '6': '\x1e',
    '7': '\x1f',
    '8': '\x7f',
  }
  return map[c] ?? ch
}

// modParam is the xterm/DEC modifier parameter: 1 + shift + alt + ctrl.
function modParam({ shift, alt, ctrl }) {
  return 1 + (shift ? 1 : 0) + (alt ? 2 : 0) + (ctrl ? 4 : 0)
}

// csiKey builds a cursor / navigation sequence, folding in any active
// modifiers using the standard CSI 1;<m><final> form.
function csiKey(finalOrTilde, mods) {
  const m = modParam(mods)
  const isTilde = /~$/.test(finalOrTilde) // e.g. "3~" for Delete
  if (isTilde) {
    const num = finalOrTilde.slice(0, -1)
    return m === 1 ? `\x1b[${num}~` : `\x1b[${num};${m}~`
  }
  return m === 1 ? `\x1b[${finalOrTilde}` : `\x1b[1;${m}${finalOrTilde}`
}

// KEY_SEQUENCES: base sequences for named keys with no modifiers.
const F_KEYS = {
  F1: '\x1bOP',
  F2: '\x1bOQ',
  F3: '\x1bOR',
  F4: '\x1bOS',
  F5: '\x1b[15~',
  F6: '\x1b[17~',
  F7: '\x1b[18~',
  F8: '\x1b[19~',
  F9: '\x1b[20~',
  F10: '\x1b[21~',
  F11: '\x1b[23~',
  F12: '\x1b[24~',
}

/**
 * Wire the mobile console.
 *
 * @param {object} o
 * @param {HTMLElement} o.shellEl      the `#term-shell` flex column
 * @param {HTMLElement} o.xtermEl      the `#xterm` mount
 * @param {import('@xterm/xterm').Terminal} o.term
 * @param {(data: string) => void} o.sendData  send raw bytes to the PTY (write-token checked upstream)
 * @param {() => void} o.refit         re-fit xterm + send a resize
 * @returns {{ transformOutgoing: (data: string) => string, destroy: () => void, isActive: boolean }}
 */
export function setupMobileConsole({ shellEl, xtermEl, term, sendData, refit }) {
  if (!isTouchDevice() || !shellEl || !term) {
    return { transformOutgoing: (d) => d, destroy: () => {}, isActive: false }
  }

  const mods = { ctrl: false, alt: false, shift: false }
  let fnBtn = null

  // ---- toolbar DOM -------------------------------------------------
  const bar = document.createElement('div')
  bar.id = 'term-mobile-bar'
  bar.className = 'term-mobile-bar'
  bar.setAttribute('role', 'toolbar')
  bar.setAttribute('aria-label', t('terminal.mobileBarLabel'))

  const primary = document.createElement('div')
  primary.className = 'term-mobile-row'
  const fnRow = document.createElement('div')
  fnRow.className = 'term-mobile-row term-mobile-fn hidden'

  // btn builds one toolbar key. kind: 'mod' (sticky) | 'key' (action).
  function btn(label, opts = {}) {
    const b = document.createElement('button')
    b.type = 'button'
    b.className = 'term-mobile-key' + (opts.wide ? ' term-mobile-key-wide' : '')
    b.textContent = label
    if (opts.title) b.title = opts.title
    if (opts.mod) b.dataset.mod = opts.mod
    // Act on pointerdown, not click: preventDefault here keeps the
    // terminal's hidden textarea focused (so the OS keyboard is not
    // dismissed on every key), but on real touch it ALSO suppresses the
    // synthesized click -- so binding the action to click meant keys
    // like Tab and Ctrl never fired. Fire on pointerdown and swallow the
    // trailing click so the action runs exactly once.
    let firedFromPointer = false
    b.addEventListener('pointerdown', (e) => {
      e.preventDefault()
      firedFromPointer = true
      opts.onPress?.()
      refocus()
    })
    b.addEventListener('click', (e) => {
      e.preventDefault()
      if (firedFromPointer) {
        firedFromPointer = false
        return
      }
      // No preceding pointerdown (e.g. keyboard / assistive activation).
      opts.onPress?.()
      refocus()
    })
    return b
  }

  function refocus() {
    try {
      term.focus()
    } catch {
      /* ignore */
    }
  }

  function renderModStates() {
    bar.querySelectorAll('[data-mod]').forEach((el) => {
      el.classList.toggle('is-active', !!mods[el.dataset.mod])
    })
  }

  function toggleMod(name) {
    mods[name] = !mods[name]
    renderModStates()
  }

  function clearOneShots() {
    if (mods.ctrl || mods.alt || mods.shift) {
      mods.ctrl = mods.alt = mods.shift = false
      renderModStates()
    }
  }

  // sendNamed sends a named key sequence, folding in the armed
  // modifiers, then releases them.
  function sendNamed(seq) {
    let out = seq
    if (mods.alt && !seq.startsWith('\x1b')) out = '\x1b' + out
    sendData(out)
    clearOneShots()
  }

  function sendCursor(finalOrTilde) {
    sendData(csiKey(finalOrTilde, mods))
    clearOneShots()
  }

  // sendChar sends a literal character from the toolbar, applying armed
  // Ctrl / Alt exactly like a keyboard character would.
  function sendChar(ch) {
    sendData(applyMods(ch))
    clearOneShots()
  }

  // applyMods folds armed Ctrl/Alt into a single character. Shift is
  // left to the character itself (the toolbar chars are already shifted
  // where relevant).
  function applyMods(ch) {
    let out = ch
    if (mods.ctrl && out.length === 1) out = ctrlByte(out)
    if (mods.alt) out = '\x1b' + out
    return out
  }

  // Primary row: the keys reached for most often.
  primary.append(
    btn(t('terminal.keyEsc'), { onPress: () => sendNamed('\x1b'), title: 'Escape' }),
    btn('Tab', { onPress: () => sendNamed(mods.shift ? '\x1b[Z' : '\t'), title: 'Tab' }),
    btn('Ctrl', { mod: 'ctrl', onPress: () => toggleMod('ctrl') }),
    btn('Alt', { mod: 'alt', onPress: () => toggleMod('alt') }),
    btn('Shift', { mod: 'shift', onPress: () => toggleMod('shift') }),
    btn('◀', { onPress: () => sendCursor('D'), title: 'Left' }),
    btn('▼', { onPress: () => sendCursor('B'), title: 'Down' }),
    btn('▲', { onPress: () => sendCursor('A'), title: 'Up' }),
    btn('▶', { onPress: () => sendCursor('C'), title: 'Right' }),
    btn('^C', { onPress: () => sendData('\x03'), title: 'Ctrl+C' }),
    btn('Del', { onPress: () => sendCursor('3~'), title: 'Delete' }),
    btn('/', { onPress: () => sendChar('/') }),
    btn('-', { onPress: () => sendChar('-') }),
    btn('|', { onPress: () => sendChar('|') }),
    btn('~', { onPress: () => sendChar('~') }),
    (fnBtn = btn('Fn', {
      onPress: () => {
        fnRow.classList.toggle('hidden')
        fnBtn.classList.toggle('is-active', !fnRow.classList.contains('hidden'))
      },
      title: t('terminal.keyFnTitle'),
    })),
    btn('⌄', { onPress: () => hideKeyboard(), title: t('terminal.keyHideKeyboard') }),
  )

  // Function-key / extra-navigation row (toggled by Fn).
  fnRow.append(
    btn('Home', { onPress: () => sendCursor('H') }),
    btn('End', { onPress: () => sendCursor('F') }),
    btn('PgUp', { onPress: () => sendCursor('5~') }),
    btn('PgDn', { onPress: () => sendCursor('6~') }),
    btn('Ins', { onPress: () => sendCursor('2~') }),
    ...Object.keys(F_KEYS).map((k) => btn(k, { onPress: () => sendNamed(F_KEYS[k]) })),
  )

  bar.append(primary, fnRow)
  shellEl.append(bar)
  renderModStates()

  // ---- keep the console visible above the keyboard -----------------
  // The page root is normally a flex child sized by 100% of the layout
  // viewport, which on mobile is TALLER than the visible area (the
  // browser URL bar) and does not shrink when the keyboard opens -- so
  // the toolbar at its bottom ends up off-screen or behind the keyboard.
  // We instead pin the root as a fixed box exactly over the visual
  // viewport (window.visualViewport), which excludes both the browser
  // chrome and the on-screen keyboard. xterm (flex-1) fills the middle
  // and the toolbar (shrink-0) rides just above the keyboard.
  const root = shellEl.closest('.terminal-page-root') || shellEl.parentElement
  const vv = window.visualViewport

  function applyViewport() {
    if (!root) return
    const h = vv ? vv.height : window.innerHeight
    const top = vv ? vv.offsetTop : 0
    const left = vv ? vv.offsetLeft : 0
    root.style.position = 'fixed'
    root.style.top = Math.round(top) + 'px'
    root.style.left = Math.round(left) + 'px'
    root.style.right = 'auto'
    root.style.width = '100%'
    root.style.height = Math.round(h) + 'px'
    root.style.zIndex = '40'
    try {
      refit?.()
    } catch {
      /* ignore */
    }
  }

  function hideKeyboard() {
    try {
      term.blur?.()
      const ta = xtermEl?.querySelector('textarea')
      ta?.blur()
    } catch {
      /* ignore */
    }
  }

  let rafPending = false
  function onViewportChange() {
    if (rafPending) return
    rafPending = true
    requestAnimationFrame(() => {
      rafPending = false
      applyViewport()
    })
  }

  if (vv) {
    vv.addEventListener('resize', onViewportChange)
    vv.addEventListener('scroll', onViewportChange)
  }
  window.addEventListener('resize', onViewportChange)
  window.addEventListener('orientationchange', onViewportChange)
  // The keyboard often only moves the visual viewport a beat after the
  // textarea gains/loses focus; re-apply a few times to catch the
  // settled geometry.
  function nudge() {
    applyViewport()
    setTimeout(applyViewport, 150)
    setTimeout(applyViewport, 400)
  }
  xtermEl?.addEventListener('focusin', nudge)
  xtermEl?.addEventListener('focusout', nudge)
  applyViewport()
  setTimeout(applyViewport, 200)

  document.documentElement.classList.add('term-mobile-active')

  function destroy() {
    if (vv) {
      vv.removeEventListener('resize', onViewportChange)
      vv.removeEventListener('scroll', onViewportChange)
    }
    window.removeEventListener('resize', onViewportChange)
    window.removeEventListener('orientationchange', onViewportChange)
    xtermEl?.removeEventListener('focusin', nudge)
    xtermEl?.removeEventListener('focusout', nudge)
    if (root) {
      root.style.position = ''
      root.style.top = ''
      root.style.left = ''
      root.style.right = ''
      root.style.width = ''
      root.style.height = ''
      root.style.zIndex = ''
    }
    document.documentElement.classList.remove('term-mobile-active')
    try {
      bar.remove()
    } catch {
      /* ignore */
    }
  }

  // transformOutgoing folds armed Ctrl/Alt into a character typed on the
  // OS keyboard. Android soft keyboards often emit a stray empty or
  // composition event just before the real keystroke; clearing the armed
  // modifier on those would drop it before the user's key arrived (the
  // "Ctrl then c just types c" bug), so the modifier is released ONLY
  // when an actual single character is folded. Empty / non-single input
  // (composition ticks, some pastes) passes through with the modifier
  // still armed for the next real key.
  function transformOutgoing(data) {
    if ((!mods.ctrl && !mods.alt) || !data) return data
    if (data.length === 1) {
      const out = applyMods(data)
      clearOneShots()
      return out
    }
    return data
  }

  return { transformOutgoing, destroy, isActive: true }
}
