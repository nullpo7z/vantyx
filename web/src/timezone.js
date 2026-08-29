/**
 * @file User timezone preference for the Vantyx SPA.
 *
 * Mirrors the design of i18n.js: the active timezone is persisted to
 * `localStorage['vantyx_timezone']` as an IANA zone name (e.g.
 * `'Asia/Tokyo'`), or `''` for "no preference" (formatting then falls
 * back to the browser's local zone, i.e. `Intl`'s default). {@link
 * setTimezone} is for user-driven changes (persists locally + syncs to
 * the server); {@link applyServerTimezone} applies a value the server
 * already told us about (login / GET /api/me) without round-tripping.
 *
 * Keep this module dependency-free so it can be imported from any page
 * without pulling in the full SPA shell.
 */

const STORAGE_KEY = 'vantyx_timezone'

/** Timezone names to offer in the settings dropdown, beyond "Auto". */
export const SUPPORTED_TIMEZONES = (() => {
  try {
    if (typeof Intl.supportedValuesOf === 'function') {
      return Intl.supportedValuesOf('timeZone')
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

/**
 * Optional hook invoked when the user explicitly switches timezone.
 * The host application (main.js) registers a function that PUTs the new
 * timezone to `/api/me/timezone` so the preference persists across
 * devices. Kept dependency-free: the hook is registered from outside.
 *
 * @type {((timezone: string) => void | Promise<void>) | null}
 */
let serverSyncHandler = null

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
 * Return the currently active timezone preference, or `''` when the
 * user has no preference set (callers should then fall back to the
 * browser's local zone by omitting `timeZone` from Intl options).
 *
 * @returns {string}
 */
export function getTimezone() {
  return activeTimezone
}

/**
 * Best-effort guess at the browser's local IANA zone name, used to
 * pre-select something sensible in the settings dropdown when the user
 * has no saved preference yet.
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
 * Switch the active timezone because the **user** chose it from a UI
 * control. Persists locally and notifies the server (via the handler
 * registered with {@link registerServerSync}), so the preference
 * follows the user across devices.
 *
 * @param {string} tz - IANA zone name (e.g. `'Asia/Tokyo'`), or `''` to clear.
 * @returns {string} The timezone that ended up active.
 */
export function setTimezone(tz) {
  const next = applyTimezoneLocal(tz)
  if (next === tz && serverSyncHandler) {
    try {
      const ret = serverSyncHandler(tz)
      if (ret && typeof ret.catch === 'function') {
        ret.catch(() => {
          /* server failures shouldn't block local UI */
        })
      }
    } catch {
      /* defensive: handler errors shouldn't bubble into UI code */
    }
  }
  return next
}

/**
 * Apply a timezone supplied by the server (e.g. via the login or
 * GET /api/me response). Behaves like {@link setTimezone} **but does
 * not round-trip back to the server**, so the same value isn't written
 * twice when the SPA boots.
 *
 * Pass an empty string / null / `undefined` to keep the current value.
 *
 * @param {string | null | undefined} tz
 * @returns {string}
 */
export function applyServerTimezone(tz) {
  if (!tz) return activeTimezone
  return applyTimezoneLocal(tz)
}

/**
 * Register a function to be called when the user explicitly switches
 * timezone. The host application uses this to PUT the new value to
 * `/api/me/timezone` without coupling this module to the API layer.
 *
 * @param {((timezone: string) => void | Promise<void>) | null} fn
 */
export function registerServerSync(fn) {
  serverSyncHandler = typeof fn === 'function' ? fn : null
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
