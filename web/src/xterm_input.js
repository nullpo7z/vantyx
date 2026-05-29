/**
 * @file Keyboard and focus helpers for xterm.js in Vantyx browser terminals.
 *
 * xterm.js already sends PTY input via `terminal.onData` and relies on the
 * server for echo. This module only handles focus and browser shortcut routing.
 */

/**
 * @param {import('@xterm/xterm').Terminal} term
 * @param {HTMLElement} rootEl
 */
export function setupTerminalKeyboard(term, rootEl) {
  if (!term || !rootEl) return

  rootEl.addEventListener('pointerdown', () => {
    try {
      term.focus()
    } catch {
      /* ignore */
    }
  })

  term.attachCustomKeyEventHandler((ev) => {
    if (ev.type !== 'keydown') return true

    const mod = ev.ctrlKey || ev.metaKey
    if (!mod) return true

    const key = ev.key.length === 1 ? ev.key.toLowerCase() : ev.key

    // Let the browser handle tab/window shortcuts (Ctrl/Cmd+T/N/W/R).
    if (key === 't' || key === 'n' || key === 'w' || key === 'r') {
      return false
    }

    return true
  })
}
