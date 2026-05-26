// Package ja contains the Japanese translation catalog for the
// backend HTTP API error messages. Mirrors the keys defined in the
// sibling `en` package.
package ja

// Messages maps a translation key to its Japanese template. See the
// English catalog for why the declaration is annotated with
// `#nosec G101`: the values are localization strings, not secrets,
// but keys such as `auth.invalidCredentials` match gosec's hardcoded
// credentials heuristic.

// #nosec G101 -- translation strings, not secrets.
var Messages = map[string]string{
	// Generic.
	"common.unauthorized":       "認証されていません",
	"common.forbidden":          "アクセスが許可されていません",
	"common.forbiddenAdminOnly": "管理者のみ利用できます",
	"common.invalidRequestBody": "リクエスト本文が不正です",
	"common.internalError":      "内部エラーが発生しました",
	"common.serviceUnavailable": "サービスを利用できません",
	"common.notFound":           "見つかりません",
	"common.targetIDRequired":   "target_id は必須です",
	"common.targetNotFound":     "ターゲットが見つかりません",

	// Authentication.
	"auth.invalidCredentials":     "ユーザー名またはパスワードが正しくありません",
	"auth.tooManyAttempts":        "ログイン試行回数が多すぎます。しばらくしてから再度お試しください",
	"auth.sessionCreateFailed":    "セッションの作成に失敗しました",
	"auth.currentPasswordWrong":   "現在のパスワードが正しくありません",
	"auth.passwordUnchanged":      "新しいパスワードは現在のものと異なる必要があります",
	"auth.invalidSSHKey":          "SSH 公開鍵が不正です",
	"auth.sshKeyNotFound":         "鍵が見つかりません",
	"auth.sshKeyAuthorizedKeyReq": "authorized_key は必須です",
	"auth.sshKeyIDInvalid":        "key_id が不正です",
	"auth.unsupportedLocale":      "サポートされていない言語コードです",

	// Users (admin management).
	"users.idRequired":       "user_id は必須です",
	"users.userNotFound":     "ユーザーが見つかりません",
	"users.usernameRequired": "ユーザー名は必須です",
	"users.passwordRequired": "パスワードは必須です",
	"users.alreadyExists":    "同じ ID またはユーザー名のユーザーが既に存在します",
}
