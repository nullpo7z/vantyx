/**
 * @file Lightweight client-side validators used by the SPA modals.
 *
 * Each validator returns a Japanese error message when validation
 * fails, and `null` when the value is acceptable. UI text is in
 * Japanese per project policy; backend-facing strings are not
 * translated.
 */

import { ID_MAX_LENGTH, ID_PATTERN } from './constants.js'

/**
 * Validate an optional user-supplied ID against the backend's slug
 * pattern (`[A-Za-z0-9_-]{1,512}`). Empty input is accepted so that
 * callers can fall back to auto-generated IDs.
 *
 * @param {string} rawId - User input from the ID field. May be empty.
 * @returns {string | null} Japanese error message, or `null` when valid.
 */
export function validateOptionalUserId(rawId) {
  if (!rawId) return null
  if (rawId.length > ID_MAX_LENGTH || !ID_PATTERN.test(rawId)) {
    return 'ユーザーIDは英数字・ハイフン・アンダースコアのみ、最大512文字で入力してください'
  }
  return null
}
