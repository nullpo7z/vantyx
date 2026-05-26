/**
 * @file Lightweight client-side validators used by the SPA modals.
 *
 * Each validator returns a localized error message when validation
 * fails, and `null` when the value is acceptable. The message text is
 * resolved through {@link i18n.t} so it follows the active locale.
 */

import { ID_MAX_LENGTH, ID_PATTERN } from './constants.js'
import { t } from './i18n.js'

/**
 * Validate an optional user-supplied ID against the backend's slug
 * pattern (`[A-Za-z0-9_-]{1,512}`). Empty input is accepted so that
 * callers can fall back to auto-generated IDs.
 *
 * @param {string} rawId - User input from the ID field. May be empty.
 * @returns {string | null} Localized error message, or `null` when valid.
 */
export function validateOptionalUserId(rawId) {
  if (!rawId) return null
  if (rawId.length > ID_MAX_LENGTH || !ID_PATTERN.test(rawId)) {
    return t('users.validateUserId')
  }
  return null
}
