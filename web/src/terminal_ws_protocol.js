/**
 * @file WebSocket control-frame helpers for /ws/ssh (terminal bridge).
 *
 * Metadata frames are prefixed with `vantyx:meta:` so they are never
 * mistaken for shell output and written into xterm.
 */

export const TERMINAL_WS_META_PREFIX = 'vantyx:meta:'
export const TERMINAL_WS_READY = 'vantyx:ready'

/**
 * @param {string | ArrayBuffer | Blob} data
 * @returns {Promise<string>}
 */
export async function decodeWsText(data) {
  if (typeof data === 'string') return data
  if (data instanceof ArrayBuffer) {
    return new TextDecoder().decode(data)
  }
  if (typeof Blob !== 'undefined' && data instanceof Blob) {
    return data.text()
  }
  return ''
}

/**
 * @param {string} text
 * @returns {{ kind: 'empty' | 'ready' | 'meta' | 'terminal' | 'swallow', object?: object, text?: string }}
 */
export function classifyTerminalWsTextFrame(text) {
  const s = typeof text === 'string' ? text : ''
  const trimmed = s.trim()
  if (!trimmed) return { kind: 'empty' }
  if (trimmed === TERMINAL_WS_READY) return { kind: 'ready' }

  if (trimmed.startsWith(TERMINAL_WS_META_PREFIX)) {
    try {
      const object = JSON.parse(trimmed.slice(TERMINAL_WS_META_PREFIX.length))
      if (object && typeof object === 'object') return { kind: 'meta', object }
    } catch {
      return { kind: 'swallow' }
    }
    return { kind: 'swallow' }
  }

  // Legacy bare JSON control (older servers); never render in xterm.
  if (trimmed.startsWith('{')) {
    try {
      const object = JSON.parse(trimmed)
      if (object && typeof object === 'object') {
        if (typeof object.session_id === 'string' || object.type?.startsWith?.('host_key_')) {
          return { kind: 'meta', object }
        }
      }
    } catch {
      /* treat as terminal text */
    }
  }

  return { kind: 'terminal', text: s }
}

/**
 * @param {string | ArrayBuffer} data
 */
export function classifyTerminalWsFrameSync(data) {
  if (typeof data === 'string') return classifyTerminalWsTextFrame(data)
  if (data instanceof ArrayBuffer) {
    return classifyTerminalWsTextFrame(new TextDecoder().decode(data))
  }
  return { kind: 'binary' }
}
