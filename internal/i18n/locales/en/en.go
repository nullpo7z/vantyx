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
	"common.unauthorized":            "unauthorized",
	"common.forbidden":               "forbidden",
	"common.forbiddenAdminOnly":      "forbidden: admin only",
	"common.invalidRequestBody":      "invalid request body",
	"common.invalidJSON":             "invalid JSON",
	"common.internalError":           "internal error",
	"common.gatewayFailed":           "connection to the target failed",
	"common.serviceUnavailable":      "service unavailable",
	"common.notFound":                "not found",
	"common.methodNotAllowed":        "method not allowed",
	"common.invalidPath":             "invalid path",
	"common.pathRequired":            "path is required",
	"common.fileNotFound":            "file not found",
	"common.invalidAfterID":          "invalid after_id",
	"common.failedUpgradeConnection": "failed to upgrade connection",
	"common.cannotDownloadDirectory": "cannot download a directory",
	"common.targetIDRequired":        "target_id required",
	"common.targetNotFound":          "target not found",

	// Authentication.
	"auth.invalidCredentials":     "invalid credentials",
	"auth.tooManyAttempts":        "too many failed attempts; try again later",
	"auth.sessionCreateFailed":    "failed to create session",
	"auth.currentPasswordWrong":   "current password is wrong",
	"auth.passwordUnchanged":      "new password must differ from current",
	"auth.passwordEmpty":          "password must not be empty",
	"auth.passwordTooShort":       "password must be at least {min} characters",
	"auth.passwordNoUpper":        "password must contain at least one uppercase letter",
	"auth.passwordNoLower":        "password must contain at least one lowercase letter",
	"auth.passwordNoDigit":        "password must contain at least one digit",
	"auth.passwordNoSpecial":      "password must contain at least one special character",
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

	// Groups.
	"groups.idRequired":             "group_id required",
	"groups.notFound":               "group not found",
	"groups.nameRequired":           "name is required",
	"groups.userIDRequired":         "user_id is required",
	"groups.groupAndUserIDRequired": "group_id and user_id required",

	// Targets.
	"targets.nameHostRequired":             "name and host are required",
	"targets.credentialSourceExclusive":      "specify either credential_identity_id or ssh_key_id, not both",
	"targets.groupIDRequired":           "group_id is required",
	"targets.groupNotFound":             "group not found",
	"targets.assignFailed":              "failed to assign target to group",
	"targets.protocolInvalid":           "protocol must be ssh, telnet, vnc, tftp, ftp, or rdp",
	"targets.hostKeyFingerprintInvalid": "ssh_host_key_fingerprint must be empty or in 'SHA256:<base64>' form",
	"targets.hostKeyOnlySSH":            "host key fingerprint is only meaningful for SSH targets",
	"targets.hostKeyProbeFailed":        "failed to probe upstream SSH host key",

	// Settings.
	"settings.invalidProto":  "invalid proto",
	"settings.invalidBuffer": "invalid buffer",
	"settings.addrRequired":  "addr is required",
	"settings.saveFailed":    "failed to save settings",

	// Sessions / terminal.
	"sessions.idRequired":             "session_id required",
	"sessions.notFoundOrAccessDenied": "session not found or access denied",
	"sessions.onlySSHTelnet":          "only SSH and Telnet targets supported",
	"sessions.eventsUnavailable":      "session events not available",

	// Collaborative session sharing (Phase A).
	"sharing.unavailable":            "collaborative sessions are not available",
	"sharing.modeInvalid":            "invitation mode is invalid",
	"sharing.inviteeNotFound":        "invited user not found",
	"sharing.inviteeOrTagOnly":       "cannot specify both a user and a tag",
	"sharing.inviteTagInvalid":       "tag is invalid",
	"sharing.inviteTagNotForTarget":  "this tag does not grant access to the target",
	"sharing.inviteeNoTargetAccess":  "invited user cannot access this target",
	"sharing.linkMaxUsesInvalid":     "invalid link usage limit",
	"sharing.cannotInviteSelf":       "you cannot invite yourself",
	"sharing.tokenOrIDRequired":      "invitation token or id required",
	"sharing.invitationNotFound":     "invitation not found",
	"sharing.invitationInactive":     "invitation is no longer active",
	"sharing.invitationOtherUser":    "invitation is for a different user",
	"sharing.invitationStaleAccess":  "inviter no longer has access to the target",
	"sharing.userIDRequired":         "user_id required",
	"sharing.cannotKickOwner":        "the session owner cannot be removed",
	"sharing.participantNotFound":    "participant not found",
	"sharing.alreadyWriter":          "you already hold the write token",
	"sharing.requestIDRequired":      "request_id required",
	"sharing.writeRequestNotFound":   "write request not found",
	"sharing.writeRequestNotPending": "write request has already been decided",
	"sharing.notWriter":              "only the current writer can do this",

	// Command logs.
	"command.queryFailed": "failed to query command logs",

	// Recordings.
	"recordings.idRequired":         "recording_id required",
	"recordings.formatInvalid":      "format must be cast, gif, or webm",
	"recordings.notAvailable":       "recordings not available",
	"recordings.notFound":           "recording not found",
	"recordings.notConfigured":      "recordings not configured",
	"recordings.fileNotFound":       "recording file not found",
	"recordings.convertReadFailed":  "failed to read converted file",
	"recordings.videoExportFailed":  "Video export failed.",
	"recordings.videoToolsRequired": "Video export is unavailable. The server needs agg and ffmpeg installed.",

	// Credential profiles (admin management).
	"credentials.encryptionKeyRequired": "encryption key not configured (VANTYX_SSH_PASSWORD_ENCRYPTION_KEY)",
	"credentials.notReady":              "credential library is not ready. The server may require a database migration after update.",

	// SSH keys / identities (admin management).
	"sshKeys.idRequired":                  "key_id required",
	"sshKeys.notFound":                    "ssh key not found",
	"sshKeys.exists":                      "ssh key already exists",
	"sshKeys.inUse":                       "ssh key is referenced by an identity",
	"sshKeys.privateKeyRequired":          "private key required",
	"credentialIdentities.idRequired":     "identity_id required",
	"credentialIdentities.notFound":       "identity not found",
	"credentialIdentities.exists":         "identity already exists",
	"credentialIdentities.usernameRequired": "username required",
	"credentialIdentities.authRequired":     "password and/or ssh key required",

	// RDP / VNC / TFTP capability checks.
	"rdp.notRDP":         "target is not an RDP server",
	"rdp.startFailed":    "failed to start RDP session",
	"rdp.bridgeFailed":   "failed to start RDP bridge",
	"vnc.notVNC":         "target is not a VNC server",
	"tftp.notTFTPServer": "target is not a TFTP server",

	// File operations (SFTP / FTP / TFTP).
	"files.transferOnlySSHFTPTFTP":   "file transfer only for SSH, FTP, or TFTP targets",
	"files.sftpDisabled":             "SFTP file transfer is disabled for this target",
	"files.ftpDisabled":              "FTP file transfer is disabled for this target",
	"files.credentialsRequired":      "stored credentials (password or SSH key) required for file transfer",
	"files.ftpCredentialsRequired":   "stored username and password are required for FTP file transfer",
	"files.credentialsDecryptFailed": "failed to decrypt stored credentials; verify VANTYX_SSH_PASSWORD_ENCRYPTION_KEY",
	"files.connectFailed":            "failed to connect to target",
	"files.listFailed":               "list failed",
	"files.openFailed":               "open failed",
	"files.statFailed":               "stat failed",
	"files.createFailed":             "create failed",
	"files.uploadFailed":             "upload failed",
	"files.removeFailed":             "remove failed",
	"files.cannotDeleteRoot":         "cannot delete root",
	"files.tftpDeleteUnsupported":    "TFTP does not support delete",
	"files.invalidMultipart":         "invalid multipart form",
	"files.fileRequired":             "file is required",
	"files.directoryNotEmpty":        "directory is not empty",

	// File transfers (async job API).
	"transfers.notADownload":          "not a download transfer",
	"transfers.notReady":              "transfer not ready",
	"transfers.backendInvalid":        "backend must be remote or tftp_server",
	"transfers.targetAndPathReq":      "target_id and path are required",
	"transfers.notSupportedForTarget": "file transfer not supported for this target",
	"transfers.eventsUnavailable":     "file transfer events not available",
	"transfers.invalidCursor":         "invalid after_id cursor",
	"transfers.invalidState":          "invalid state filter",
	"transfers.invalidDirection":      "direction must be upload or download",
	"transfers.invalidBackend":        "backend must be remote or tftp_server",

	// Validation (access / auth identifier and field constraints).
	"validation.groupIDEmpty":    "group id must not be empty",
	"validation.groupIDTooLong":  "group id too long",
	"validation.groupIDInvalid":  "group id contains invalid characters",
	"validation.targetIDEmpty":   "target id must not be empty",
	"validation.targetIDTooLong": "target id too long",
	"validation.targetIDInvalid": "target id contains invalid characters",
	"validation.nameEmpty":       "name must not be empty",
	"validation.nameTooLong":     "name too long",
	"validation.nameInvalid":     "name contains invalid characters",
	"validation.hostEmpty":       "host must not be empty",
	"validation.hostTooLong":     "host too long",
	"validation.hostInvalid":     "host must be a valid hostname or IP address",
	"validation.idUsernameEmpty": "id and username must not be empty",

	// Tag validation (group / target / user tags share these keys).
	"tags.lengthInvalid": "tag must be 1–64 characters",
	"tags.charsInvalid":  "tag may only contain letters, numbers, hyphen, underscore",

	// Proxy / bridge dial failures (see internal/proxyerrors).
	"proxy.tcpTimeout":         "TCP connection to {proto} timed out. Check that the service is enabled, firewall/ACL/port settings, and network path from Vantyx to the target.",
	"proxy.connectionRefused":  "Connection to {proto} was refused. Check that the service is running and the port number is correct.",
	"proxy.noRoute":            "No route to {proto}. Check IP address, VLAN, and routing.",
	"proxy.networkUnreachable": "Network is unreachable. Check the path from the Vantyx server to the target.",
	"proxy.dialFailed":         "Failed to connect to {proto}. Check host, port, and that the service is running.",

	// Time range query parameters.
	"time.invalidFrom":    "invalid from: {reason}",
	"time.invalidTo":      "invalid to: {reason}",
	"time.fromBeforeTo":   "from must be before to",
	"time.rangeTooLarge":  "time range must not exceed {days} days",
	"time.invalidRange":   "invalid time range",
	"time.expectedFormat": "expected RFC3339 or YYYY-MM-DD",
}
