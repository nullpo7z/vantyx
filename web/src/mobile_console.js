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
    // Keep the terminal's hidden textarea focused: acting on pointerdown
    // and preventing default stops the tap from stealing focus (which
    // would dismiss the OS keyboard on every key press).
    b.addEventListener('pointerdown', (e) => {
      e.preventDefault()
    })
    b.addEventListener('click', (e) => {
      e.preventDefault()
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
  const root = shellEl.closest('.terminal-page-root') || shellEl.parentElement
  const vv = window.visualViewport

  function applyViewport() {
    if (!vv || !root) return
    // Pin the whole page to the visible band: height = visible height,
    // shifted down by however far the layout viewport scrolled under the
    // keyboard. The toolbar (shrink-0) then rides just above the
    // keyboard and xterm (flex-1) fills the rest.
    root.style.height = Math.round(vv.height) + 'px'
    root.style.transform = `translateY(${Math.round(vv.offsetTop)}px)`
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
    applyViewport()
  }

  document.documentElement.classList.add('term-mobile-active')

  function destroy() {
    if (vv) {
      vv.removeEventListener('resize', onViewportChange)
      vv.removeEventListener('scroll', onViewportChange)
    }
    if (root) {
      root.style.height = ''
      root.style.transform = ''
    }
    document.documentElement.classList.remove('term-mobile-active')
    try {
      bar.remove()
    } catch {
      /* ignore */
    }
  }

  // transformOutgoing folds armed modifiers into a single character
  // typed on the OS keyboard, then releases the one-shots. Multi-byte
  // input (IME, paste) passes through unchanged but still clears any
  // armed modifier so it does not leak onto later input.
  function transformOutgoing(data) {
    if (!mods.ctrl && !mods.alt) return data
    let out = data
    if (data.length === 1) out = applyMods(data)
    clearOneShots()
    return out
  }

  return { transformOutgoing, destroy, isActive: true }
}
