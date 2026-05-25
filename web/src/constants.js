/**
 * @file Application-wide constants shared between modules.
 *
 * Kept in a single file so that string literals used to coordinate
 * between the SPA shell, page modules, and the backend's slug
 * validation are not duplicated.
 */

/**
 * Internal tag that marks a target as having TFTP enabled (matches the
 * "Enable TFTP" checkbox in the server management UI).
 */
export const TFTP_CAPABILITY_TAG = 'tftp_enabled'

/**
 * Internal tag attached to SSH targets when the operator disables SFTP
 * under "File transfer protocols".
 */
export const SFTP_DISABLED_TAG = 'no-sftp'

/**
 * Maximum length accepted by the backend's ID validation. Mirrors
 * `[a-zA-Z0-9_\-]{1,512}` enforced server-side.
 */
export const ID_MAX_LENGTH = 512

/** Regular expression matching valid IDs (letters, digits, `_`, `-`). */
export const ID_PATTERN = /^[A-Za-z0-9_-]+$/

/**
 * Tailwind class string used by the main pane in tree-style pages
 * (home, server management, recordings).
 */
export const TREE_MAIN_CLASS = 'flex-1 overflow-auto p-6 flex flex-col items-center min-h-0'
