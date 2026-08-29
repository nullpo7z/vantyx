/**
 * @file Display timezone for the Vantyx SPA.
 *
 * The timezone is fixed server-wide by VANTYX_TIMEZONE (the same zone
 * the container logs and audit trail use) and delivered to every client
 * in the login / GET /api/me responses. It is cached in
 * `localStorage['vantyx_timezone']` as an IANA zone name (e.g.
 * `'Asia/Tokyo'`), or `''` for "browser local" (formatting then omits
 * `timeZone`, i.e. `Intl`'s default), so standalone pages can format
 * timestamps before their own `/api/me` round trip completes.
 * {@link applyServerTimezone} applies the server value; there is no
 * user-driven setter.
 *
 * Keep this module dependency-free so it can be imported from any page
 * without pulling in the full SPA shell.
 */

const STORAGE_KEY = 'vantyx_timezone'

function isValidTimezone(tz) {
  if (!tz) return false
  try {
    // Intl throws RangeError for an unknown zone name.
    return !!new Intl.DateTimeFormat(undefined, { timeZone: tz })
  } catch {
    return false
  }
}

let activeTimezone = readStoredTimezone()

function readStoredTimezone() {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === '' || isValidTimezone(v)) return v || ''
  } catch {
    /* localStorage unavailable (private mode etc.) */
  }
  return ''
}

/**
 * Return the currently active timezone, or `''` when the site uses
 * browser-local time (callers should then omit `timeZone` from Intl
 * options).
 *
 * @returns {string}
 */
export function getTimezone() {
  return activeTimezone
}

function applyTimezoneLocal(tz) {
  const next = tz === '' ? '' : (isValidTimezone(tz) ? tz : null)
  if (next === null || next === activeTimezone) {
    return activeTimezone
  }
  activeTimezone = next
  try {
    localStorage.setItem(STORAGE_KEY, next)
  } catch {
    /* localStorage unavailable */
  }
  try {
    window.dispatchEvent(new CustomEvent('timezonechange', { detail: { timezone: next } }))
  } catch {
    /* very old browsers — nothing to do */
  }
  return activeTimezone
}

/**
 * Apply the server-wide timezone supplied by the server (login /
 * GET /api/me response). The server value
 * is authoritative: an empty string switches back to browser-local
 * time. Pass `null` / `undefined` to keep the current value (response
 * without the field).
 *
 * @param {string | null | undefined} tz
 * @returns {string}
 */
export function applyServerTimezone(tz) {
  if (tz === null || tz === undefined) return activeTimezone
  return applyTimezoneLocal(String(tz))
}

/**
 * Subscribe to timezone changes.
 *
 * @param {(timezone: string) => void} listener
 * @returns {() => void} Unsubscribe handle.
 */
export function onTimezoneChange(listener) {
  const handler = (ev) => {
    try {
      listener(ev?.detail?.timezone ?? activeTimezone)
    } catch {
      /* listener errors shouldn't kill the dispatcher */
    }
  }
  window.addEventListener('timezonechange', handler)
  return () => window.removeEventListener('timezonechange', handler)
}
