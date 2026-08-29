/**
 * @file Display timezone for the Vantyx SPA.
 *
 * The timezone is a site-wide setting chosen by an administrator
 * (PUT /api/settings/timezone) and delivered to every client in the
 * login / GET /api/me responses. It is cached in
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

/** Timezone names to offer in the settings dropdown, beyond "Auto". */
export const SUPPORTED_TIMEZONES = (() => {
  try {
    if (typeof Intl.supportedValuesOf === 'function') {
      const zones = Intl.supportedValuesOf('timeZone')
      // Chrome's supportedValuesOf('timeZone') lists only region/city
      // zones and omits plain "UTC", which is the one zone an operator
      // most often wants for correlating with server logs. Always offer
      // it first (Intl accepts "UTC" as a timeZone everywhere).
      return zones.includes('UTC') ? zones : ['UTC', ...zones]
    }
  } catch {
    /* not supported in this browser */
  }
  // Small fallback list for browsers without Intl.supportedValuesOf
  // (e.g. older Safari/Firefox) so the dropdown isn't empty.
  return [
    'UTC',
    'Asia/Tokyo',
    'Asia/Shanghai',
    'Asia/Singapore',
    'Asia/Kolkata',
    'Asia/Dubai',
    'Europe/London',
    'Europe/Paris',
    'Europe/Berlin',
    'Europe/Moscow',
    'America/New_York',
    'America/Chicago',
    'America/Denver',
    'America/Los_Angeles',
    'America/Sao_Paulo',
    'Australia/Sydney',
    'Pacific/Auckland',
  ]
})()

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

/**
 * Best-effort guess at the browser's local IANA zone name, shown in the
 * admin settings dropdown next to the "browser local" option.
 *
 * @returns {string}
 */
export function detectBrowserTimezone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || ''
  } catch {
    return ''
  }
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
 * Apply the site-wide timezone supplied by the server (login /
 * GET /api/me / PUT /api/settings/timezone response). The server value
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
