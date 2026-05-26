// Package en contains the English translation catalog for the
// backend HTTP API error messages. Keys are dotted strings prefixed by
// the domain (auth.*, users.*, groups.*, …) so that handlers can pick
// related messages quickly.
//
// Keep this file ASCII-only; the Japanese catalog lives in the ja
// sibling package.
package en

// Messages maps a translation key to its English template. Templates
// may contain `{name}` placeholders which i18n.T() substitutes from
// the caller's variadic name/value pairs.
//
// The values here are user-facing localization strings (not secrets):
// keys such as `auth.invalidCredentials` and `auth.passwordUnchanged`
// trigger gosec's G101 "hardcoded credentials" heuristic on the key
// name alone, so the declaration is annotated to suppress the false
// positive.

// #nosec G101 -- translation strings, not secrets.
var Messages = map[string]string{
	// Generic.
	"common.unauthorized":       "unauthorized",
	"common.forbidden":          "forbidden",
	"common.forbiddenAdminOnly": "forbidden: admin only",
	"common.invalidRequestBody": "invalid request body",
	"common.internalError":      "internal error",
	"common.serviceUnavailable": "service unavailable",
	"common.notFound":           "not found",
	"common.targetIDRequired":   "target_id required",
	"common.targetNotFound":     "target not found",

	// Authentication.
	"auth.invalidCredentials":     "invalid credentials",
	"auth.tooManyAttempts":        "too many failed attempts; try again later",
	"auth.sessionCreateFailed":    "failed to create session",
	"auth.currentPasswordWrong":   "current password is wrong",
	"auth.passwordUnchanged":      "new password must differ from current",
	"auth.invalidSSHKey":          "invalid SSH public key",
	"auth.sshKeyNotFound":         "key not found",
	"auth.sshKeyAuthorizedKeyReq": "authorized_key is required",
	"auth.sshKeyIDInvalid":        "invalid key_id",
	"auth.unsupportedLocale":      "unsupported locale",

	// Users (admin management).
	"users.idRequired":       "user_id required",
	"users.userNotFound":     "user not found",
	"users.usernameRequired": "username is required",
	"users.passwordRequired": "password is required",
	"users.alreadyExists":    "user already exists (id or username)",
}
