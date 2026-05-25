/**
 * @file Theme management (light / dark).
 *
 * Behaviour:
 * - Preference is persisted to `localStorage['vantyx_theme']` as
 *   `'dark'` or `'light'`.
 * - When no preference is stored we follow the OS via
 *   `prefers-color-scheme: dark`.
 * - The active mode is reflected by the `dark` class on `<html>`,
 *   which Tailwind picks up.
 *
 * To avoid FOUC the same logic also runs in an inline script in
 * `web/index.html`. We re-apply it from JS on every navigation so a
 * cached `index.html` cannot leave the SPA in the wrong mode.
 */

const STORAGE_KEY = 'vantyx_theme'

/**
 * @returns {'dark' | 'light' | null} The saved preference, or `null`
 *   when none is stored (or localStorage is unavailable).
 */
function readSavedTheme() {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    return v === 'dark' || v === 'light' ? v : null
  } catch {
    return null
  }
}

/**
 * @returns {boolean} Whether the OS has requested a dark colour scheme.
 */
function prefersDarkColorScheme() {
  try {
    return Boolean(
      typeof window !== 'undefined' &&
        window.matchMedia &&
        window.matchMedia('(prefers-color-scheme: dark)').matches,
    )
  } catch {
    return false
  }
}

/**
 * Return the currently saved theme, falling back to the OS preference
 * when nothing has been stored.
 *
 * @returns {'dark' | 'light'}
 */
export function getStoredTheme() {
  const saved = readSavedTheme()
  if (saved) return saved
  return prefersDarkColorScheme() ? 'dark' : 'light'
}

/**
 * Apply the derived theme (from localStorage or `prefers-color-scheme`)
 * to `<html>`. Safe to call repeatedly after navigation; the function
 * is idempotent.
 */
export function applyStoredTheme() {
  if (typeof document === 'undefined') return
  const theme = getStoredTheme()
  const root = document.documentElement
  if (!root) return
  if (theme === 'dark') {
    root.classList.add('dark')
  } else {
    root.classList.remove('dark')
  }
}

/**
 * Toggle the active theme and persist the choice.
 *
 * @returns {'dark' | 'light'} The theme after toggling.
 */
export function toggleStoredTheme() {
  if (typeof document === 'undefined') return 'light'
  const root = document.documentElement
  const isDark = root.classList.toggle('dark')
  const next = isDark ? 'dark' : 'light'
  try {
    localStorage.setItem(STORAGE_KEY, next)
  } catch {
    /* localStorage 不可（プライベートブラウジング等）でも UI 動作は継続 */
  }
  return next
}

// Apply once on module load as a safety net for the rare case where
// the inline script in index.html did not run (cached SPA, etc.).
applyStoredTheme()
