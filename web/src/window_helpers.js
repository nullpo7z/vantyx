/**
 * @file Window / popup / localStorage helpers used by the SPA shell
 * to open terminal tabs, sized popups, and remember per-target RDP
 * resolutions.
 */

/**
 * Generate a 128-bit hex token using `crypto.getRandomValues`. Used
 * to scope a per-tab BroadcastChannel between the SPA and child
 * terminal tabs.
 *
 * @returns {string} 32-character hex string.
 */
export function randomToken() {
  const b = new Uint8Array(16)
  crypto.getRandomValues(b)
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
}

/**
 * Open a sized centred popup window (used for VNC / RDP consoles).
 * `noopener` is set so the popup cannot reach back into the parent.
 *
 * @param {string} url - Target URL.
 * @param {string} title - Popup name. Pass `'_blank'` for unique windows.
 * @param {number} [w=1280] - Width in CSS pixels.
 * @param {number} [h=800] - Height in CSS pixels.
 */
export function openPopup(url, title, w = 1280, h = 800) {
  const left = Math.max(0, Math.round((window.screen.width - w) / 2))
  const top = Math.max(0, Math.round((window.screen.height - h) / 2))
  const feats = [
    'popup=yes',
    'resizable=yes',
    'scrollbars=no',
    'noopener=yes',
    `width=${w}`,
    `height=${h}`,
    `left=${left}`,
    `top=${top}`,
  ].join(',')
  window.open(url, title || '_blank', feats)
}

/**
 * Read the saved RDP resolution for a target from localStorage,
 * defaulting to 1920x1080. Any parse error falls back to the default
 * silently so a corrupted value does not block the UI.
 *
 * @param {string} id - Target ID.
 * @returns {{w:number, h:number}} Resolution in CSS pixels.
 */
export function getRdpResolutionForTarget(id) {
  try {
    if (!id) return { w: 1920, h: 1080 }
    const raw = localStorage.getItem(`vantyx_rdp_res_${id}`)
    if (!raw) return { w: 1920, h: 1080 }
    const parsed = JSON.parse(raw)
    const w = Number(parsed.w) || 1920
    const h = Number(parsed.h) || 1080
    return { w, h }
  } catch {
    return { w: 1920, h: 1080 }
  }
}

/**
 * Persist the RDP resolution for a target. Passing `0` for either
 * dimension removes the saved value so the next open uses the default.
 *
 * @param {string} id - Target ID.
 * @param {number} w - Width in CSS pixels.
 * @param {number} h - Height in CSS pixels.
 */
export function setRdpResolutionForTarget(id, w, h) {
  try {
    if (!id) return
    const ww = Number(w) || 0
    const hh = Number(h) || 0
    if (!ww || !hh) {
      localStorage.removeItem(`vantyx_rdp_res_${id}`)
      return
    }
    localStorage.setItem(`vantyx_rdp_res_${id}`, JSON.stringify({ w: ww, h: hh }))
  } catch {
    // ignore storage errors (Safari private mode, quota exceeded, ...).
  }
}
