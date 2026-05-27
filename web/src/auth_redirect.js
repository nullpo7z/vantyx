/**
 * @file Post-login redirect helpers.
 *
 * Standalone pages (/terminal, /files, …) stash the requested URL
 * when the user is not authenticated. After a successful login on `/`,
 * the SPA navigates back to that URL so invitation links and deep
 * links keep their query parameters (session_id, invite token, etc.).
 */

const STORAGE_KEY = 'vantyx_post_login_redirect'

/**
 * @param {string} [explicit] Optional path+search+hash; defaults to current location.
 * @returns {boolean} Whether a redirect was stored.
 */
export function savePostLoginRedirect(explicit) {
  const path =
    explicit != null && explicit !== ''
      ? explicit
      : window.location.pathname + window.location.search + window.location.hash
  if (!isSafeRedirectPath(path)) return false
  if (path === '/' || path.startsWith('/?')) return false
  try {
    sessionStorage.setItem(STORAGE_KEY, path)
    return true
  } catch {
    return false
  }
}

/**
 * If the login page was opened with `?next=/terminal?...`, persist it
 * for use after authentication.
 */
export function captureNextQueryParam() {
  try {
    const next = new URLSearchParams(window.location.search).get('next')
    if (next && isSafeRedirectPath(next)) {
      sessionStorage.setItem(STORAGE_KEY, next)
    }
  } catch {
    /* ignore */
  }
}

/**
 * @returns {string | null} Stored path if any (does not consume).
 */
export function peekPostLoginRedirect() {
  try {
    const path = sessionStorage.getItem(STORAGE_KEY)
    return path && isSafeRedirectPath(path) ? path : null
  } catch {
    return null
  }
}

/**
 * Navigate to the stored post-login URL if present.
 *
 * @returns {boolean} True when a redirect was performed.
 */
export function consumePostLoginRedirect() {
  let path = null
  try {
    path = sessionStorage.getItem(STORAGE_KEY)
    sessionStorage.removeItem(STORAGE_KEY)
  } catch {
    return false
  }
  if (!path || !isSafeRedirectPath(path)) return false
  const here = window.location.pathname + window.location.search + window.location.hash
  if (path === here) return false
  window.location.assign(path)
  return true
}

/**
 * @param {string} path
 * @returns {boolean}
 */
export function isSafeRedirectPath(path) {
  if (!path || typeof path !== 'string') return false
  if (!path.startsWith('/')) return false
  if (path.startsWith('//')) return false
  if (path.includes('://')) return false
  if (path.includes('\\')) return false
  return true
}

/**
 * Build a login URL on `/` that preserves a return path via `?next=`.
 *
 * @param {string} [returnPath]
 * @returns {string}
 */
export function loginUrlWithNext(returnPath) {
  const path =
    returnPath != null && returnPath !== ''
      ? returnPath
      : window.location.pathname + window.location.search + window.location.hash
  if (!isSafeRedirectPath(path) || path === '/' || path.startsWith('/?')) {
    return '/'
  }
  const u = new URL('/', window.location.origin)
  u.searchParams.set('next', path)
  return u.pathname + u.search
}
