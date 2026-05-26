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
	"common.unauthorized":            "認証されていません",
	"common.forbidden":               "アクセスが許可されていません",
	"common.forbiddenAdminOnly":      "管理者のみ利用できます",
	"common.invalidRequestBody":      "リクエスト本文が不正です",
	"common.invalidJSON":             "JSON が不正です",
	"common.internalError":           "内部エラーが発生しました",
	"common.serviceUnavailable":      "サービスを利用できません",
	"common.notFound":                "見つかりません",
	"common.methodNotAllowed":        "許可されていないメソッドです",
	"common.invalidPath":             "パスが不正です",
	"common.pathRequired":            "パスは必須です",
	"common.fileNotFound":            "ファイルが見つかりません",
	"common.invalidAfterID":          "after_id が不正です",
	"common.failedUpgradeConnection": "コネクションのアップグレードに失敗しました",
	"common.cannotDownloadDirectory": "ディレクトリはダウンロードできません",
	"common.targetIDRequired":        "target_id は必須です",
	"common.targetNotFound":          "ターゲットが見つかりません",

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

	// Groups.
	"groups.idRequired":             "group_id は必須です",
	"groups.notFound":               "グループが見つかりません",
	"groups.nameRequired":           "name は必須です",
	"groups.userIDRequired":         "user_id は必須です",
	"groups.groupAndUserIDRequired": "group_id と user_id が必要です",

	// Targets.
	"targets.nameHostRequired": "name と host は必須です",
	"targets.groupIDRequired":  "group_id は必須です",
	"targets.groupNotFound":    "グループが見つかりません",
	"targets.assignFailed":     "ターゲットのグループ割り当てに失敗しました",

	// Settings.
	"settings.invalidProto":  "プロトコル指定が不正です",
	"settings.invalidBuffer": "バッファ指定が不正です",
	"settings.addrRequired":  "addr は必須です",
	"settings.saveFailed":    "設定の保存に失敗しました",

	// Sessions / terminal.
	"sessions.idRequired":             "session_id は必須です",
	"sessions.notFoundOrAccessDenied": "セッションが見つからないかアクセスが許可されていません",
	"sessions.onlySSHTelnet":          "SSH と Telnet のターゲットのみ対応しています",
	"sessions.eventsUnavailable":      "セッションイベントを利用できません",

	// Command logs.
	"command.queryFailed": "コマンドログの取得に失敗しました",

	// Recordings.
	"recordings.idRequired":        "recording_id は必須です",
	"recordings.formatInvalid":     "format は cast, gif, webm のいずれかである必要があります",
	"recordings.notAvailable":      "録画機能を利用できません",
	"recordings.notFound":          "録画が見つかりません",
	"recordings.notConfigured":     "録画機能が設定されていません",
	"recordings.fileNotFound":      "録画ファイルが見つかりません",
	"recordings.convertReadFailed": "変換後ファイルの読み込みに失敗しました",
	"recordings.videoUnavailable":  "動画書き出しを利用できません: {error}",

	// RDP / VNC / TFTP capability checks.
	"rdp.notRDP":         "RDP サーバーではありません",
	"rdp.startFailed":    "RDP セッションの開始に失敗しました",
	"rdp.bridgeFailed":   "RDP ブリッジの開始に失敗しました: {error}",
	"vnc.notVNC":         "VNC サーバーではありません",
	"tftp.notTFTPServer": "TFTP サーバーではありません",

	// File operations (SFTP / FTP / TFTP).
	"files.transferOnlySSHFTPTFTP":   "ファイル転送は SSH / FTP / TFTP のターゲットのみ対応しています",
	"files.sftpDisabled":             "このターゲットの SFTP ファイル転送は無効化されています",
	"files.credentialsRequired":      "ファイル転送には保存済みの認証情報（パスワードまたは SSH 鍵）が必要です",
	"files.ftpCredentialsRequired":   "FTP ファイル転送には保存済みのユーザー名とパスワードが必要です",
	"files.credentialsDecryptFailed": "保存された認証情報の復号に失敗しました。VANTYX_SSH_PASSWORD_ENCRYPTION_KEY を確認してください",
	"files.connectFailed":            "ターゲットへの接続に失敗しました: {error}",
	"files.listFailed":               "一覧取得に失敗しました: {error}",
	"files.openFailed":               "ファイルを開けませんでした: {error}",
	"files.statFailed":               "stat に失敗しました: {error}",
	"files.createFailed":             "ファイルの作成に失敗しました: {error}",
	"files.uploadFailed":             "アップロードに失敗しました: {error}",
	"files.removeFailed":             "削除に失敗しました: {error}",
	"files.cannotDeleteRoot":         "ルートは削除できません",
	"files.tftpDeleteUnsupported":    "TFTP は削除に対応していません",
	"files.invalidMultipart":         "multipart フォームが不正です: {error}",
	"files.fileRequired":             "file が必要です: {error}",
	"files.directoryNotEmpty":        "ディレクトリが空ではありません",

	// File transfers (async job API).
	"transfers.notADownload":          "ダウンロード転送ではありません",
	"transfers.notReady":              "転送の準備ができていません",
	"transfers.backendInvalid":        "backend は remote または tftp_server である必要があります",
	"transfers.targetAndPathReq":      "target_id と path は必須です",
	"transfers.notSupportedForTarget": "このターゲットではファイル転送に対応していません",
	"transfers.eventsUnavailable":     "ファイル転送イベントを利用できません",
}
