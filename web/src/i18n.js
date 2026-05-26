/**
 * @file Lightweight in-process i18n for the Vantyx SPA.
 *
 * Design:
 * - The active locale is persisted to `localStorage['vantyx_locale']`
 *   as `'en'` or `'ja'`. Default is English; we do **not** auto-detect
 *   the browser's `navigator.language` so the same UI is shown to all
 *   first-time users (admins can switch to Japanese in Settings).
 * - All available locales ship with the SPA bundle. Calling
 *   {@link setLocale} swaps the active dictionary synchronously and
 *   triggers an `i18nchange` window event so pages can re-render.
 * - {@link t} resolves a dotted key like `nav.home` against the active
 *   dictionary, falling back to English, then to the literal key.
 *   Placeholders such as `{name}` are substituted from the supplied
 *   `vars` map.
 *
 * Keep this module dependency-free so it can be imported from any
 * page without pulling in the full SPA shell.
 */

import en from './locales/en.js'
import ja from './locales/ja.js'

const STORAGE_KEY = 'vantyx_locale'
const DEFAULT_LOCALE = 'en'

/**
 * Locales bundled with the SPA. Keys are BCP-47 language tags.
 *
 * @type {Record<string, object>}
 */
const LOCALES = { en, ja }

/** Locales exposed in the language switcher. */
export const SUPPORTED_LOCALES = [
  { code: 'en', labelKey: 'language.english' },
  { code: 'ja', labelKey: 'language.japanese' },
]

let activeLocale = readStoredLocale()

/**
 * @returns {string} The locale read from localStorage, or
 *   {@link DEFAULT_LOCALE} when nothing valid is stored.
 */
function readStoredLocale() {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v && Object.prototype.hasOwnProperty.call(LOCALES, v)) {
      return v
    }
  } catch {
    /* localStorage unavailable (private mode etc.) */
  }
  return DEFAULT_LOCALE
}

/**
 * Return the currently active locale code.
 *
 * @returns {string}
 */
export function getLocale() {
  return activeLocale
}

/**
 * Switch the active locale, persist it, update `<html lang>`, and emit
 * an `i18nchange` event so listeners can re-render.
 *
 * @param {string} code - Locale code (e.g. `'en'`, `'ja'`).
 * @returns {string} The locale that ended up active (unchanged when the
 *   requested code is unknown).
 */
export function setLocale(code) {
  if (!Object.prototype.hasOwnProperty.call(LOCALES, code)) {
    return activeLocale
  }
  if (code === activeLocale) {
    return activeLocale
  }
  activeLocale = code
  try {
    localStorage.setItem(STORAGE_KEY, code)
  } catch {
    /* localStorage unavailable */
  }
  applyHtmlLangAttribute()
  try {
    window.dispatchEvent(new CustomEvent('i18nchange', { detail: { locale: code } }))
  } catch {
    /* very old browsers — nothing to do */
  }
  return activeLocale
}

/**
 * Look up the dotted key in the supplied dictionary tree.
 *
 * @param {object} tree
 * @param {string} key
 * @returns {string | undefined}
 */
function lookup(tree, key) {
  if (!key) return undefined
  const parts = key.split('.')
  let node = tree
  for (const p of parts) {
    if (node && typeof node === 'object' && p in node) {
      node = node[p]
    } else {
      return undefined
    }
  }
  return typeof node === 'string' ? node : undefined
}

/**
 * Substitute `{name}` placeholders in the supplied template.
 *
 * @param {string} template
 * @param {Record<string, unknown> | undefined} vars
 * @returns {string}
 */
function interpolate(template, vars) {
  if (!vars) return template
  return template.replace(/\{(\w+)\}/g, (_, name) => {
    if (Object.prototype.hasOwnProperty.call(vars, name)) {
      return String(vars[name] ?? '')
    }
    return `{${name}}`
  })
}

/**
 * Resolve a translation key to a string in the active locale.
 *
 * Lookup order: active locale → English → literal key.
 *
 * @param {string} key - Dotted key (e.g. `nav.home`).
 * @param {Record<string, unknown>} [vars] - Placeholder substitutions.
 * @returns {string}
 */
export function t(key, vars) {
  const fromActive = lookup(LOCALES[activeLocale], key)
  if (typeof fromActive === 'string') return interpolate(fromActive, vars)
  const fromDefault = lookup(LOCALES[DEFAULT_LOCALE], key)
  if (typeof fromDefault === 'string') return interpolate(fromDefault, vars)
  return key
}

/**
 * Reflect the active locale on `<html lang>` so accessibility tools
 * and CSS `:lang(...)` selectors stay in sync.
 */
export function applyHtmlLangAttribute() {
  if (typeof document === 'undefined' || !document.documentElement) return
  document.documentElement.lang = activeLocale
}

/**
 * Subscribe to locale changes. The listener fires after the new locale
 * has been applied (so calling {@link t} from it returns the new
 * strings).
 *
 * @param {(locale: string) => void} listener
 * @returns {() => void} Unsubscribe handle.
 */
export function onLocaleChange(listener) {
  const handler = (ev) => {
    try {
      listener(ev?.detail?.locale ?? activeLocale)
    } catch {
      /* listener errors shouldn't kill the dispatcher */
    }
  }
  window.addEventListener('i18nchange', handler)
  return () => window.removeEventListener('i18nchange', handler)
}

applyHtmlLangAttribute()
