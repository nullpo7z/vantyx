/**
 * @file Shared timestamp parsing/formatting for the Vantyx SPA.
 *
 * The backend always stores and returns timestamps in UTC, in one of
 * two shapes:
 *  - RFC3339 (`2024-01-02T03:04:05Z`), which `Date` parses correctly.
 *  - SQLite's bare `CURRENT_TIMESTAMP` format
 *    (`2024-01-02 03:04:05`, no zone marker), which browsers disagree
 *    on: some parse it as UTC, others as the browser's local zone. It
 *    IS always UTC, so it must be normalized before handing it to
 *    `Date` — otherwise the displayed time silently drifts by the
 *    viewer's UTC offset.
 *
 * {@link formatDateTime} (and friends) parse either shape correctly and
 * render using the viewer's configured timezone (see timezone.js) and
 * locale, falling back to the browser's own zone/locale when the user
 * has no preference set.
 */

import { getLocale } from './i18n.js'
import { getTimezone } from './timezone.js'

/**
 * Parse a backend timestamp string into a Date, treating SQLite's
 * space-separated `CURRENT_TIMESTAMP` format as UTC (see file header).
 *
 * @param {string | null | undefined} raw
 * @returns {Date | null} `null` when `raw` is empty or unparsable.
 */
export function parseServerTimestamp(raw) {
  if (!raw) return null
  const s = String(raw).trim()
  if (!s) return null
  // Bare "YYYY-MM-DD HH:MM:SS" (optionally with fractional seconds) has
  // no zone marker; SQLite CURRENT_TIMESTAMP / datetime('now') always
  // produce this in UTC, so normalize to a form Date parses as UTC.
  const bareUtc = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(\.\d+)?$/
  const normalized = bareUtc.test(s) ? `${s.replace(' ', 'T')}Z` : s
  const d = new Date(normalized)
  return Number.isNaN(d.getTime()) ? null : d
}

const DEFAULT_OPTIONS = {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
}

/**
 * Format a `Date` object using the viewer's configured timezone (falls
 * back to the browser's local zone when unset) and locale.
 *
 * @param {Date} d
 * @param {Intl.DateTimeFormatOptions} [options]
 * @returns {string}
 */
export function formatDate(d, options) {
  const opts = options || DEFAULT_OPTIONS
  const tz = getTimezone()
  try {
    return new Intl.DateTimeFormat(getLocale(), tz ? { ...opts, timeZone: tz } : opts).format(d)
  } catch {
    // Unknown/unsupported timeZone (shouldn't happen — values are
    // validated before being saved) — fall back to the browser default.
    return new Intl.DateTimeFormat(getLocale(), opts).format(d)
  }
}

/**
 * Format a backend timestamp for display using the viewer's configured
 * timezone and locale. Returns the placeholder string (default `'—'`)
 * when `raw` is empty/unparsable, so callers can drop this straight
 * into a table cell.
 *
 * @param {string | null | undefined} raw
 * @param {Intl.DateTimeFormatOptions} [options]
 * @param {string} [placeholder]
 * @returns {string}
 */
export function formatDateTime(raw, options, placeholder = '—') {
  const d = parseServerTimestamp(raw)
  if (!d) return placeholder
  return formatDate(d, options)
}

/**
 * Format a `Date` as the `YYYY-MM-DD` calendar date it falls on in the
 * viewer's configured timezone (browser zone when unset). Use this for
 * `<input type="date">` defaults instead of `toISOString().slice(0, 10)`,
 * which yields the UTC date and is "yesterday" for anyone east of UTC
 * in the early hours of their day.
 *
 * @param {Date} d
 * @returns {string}
 */
export function formatDateInputValue(d) {
  const tz = getTimezone()
  const opts = { year: 'numeric', month: '2-digit', day: '2-digit' }
  try {
    // en-CA renders as YYYY-MM-DD, exactly what <input type="date"> wants.
    return new Intl.DateTimeFormat('en-CA', tz ? { ...opts, timeZone: tz } : opts).format(d)
  } catch {
    return new Intl.DateTimeFormat('en-CA', opts).format(d)
  }
}
