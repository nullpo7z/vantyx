package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/secret"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

// terminalSessionIDGen is overridden in tests to force a duplicate
// session ID and exercise the Start error path.
var terminalSessionIDGen func() session.ID

// newTerminalSessionID returns a 256-bit hex-encoded random session ID.
func newTerminalSessionID() (session.ID, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return session.ID(hex.EncodeToString(b)), nil
}

// wsAuthMessage is the first WebSocket message for a new SSH / Telnet
// connection: explicit credentials or use_stored_credentials.
type wsAuthMessage struct {
	UseStoredCredentials bool   `json:"use_stored_credentials"`
	Username             string `json:"username"`
	Password             string `json:"password"`
	// PrivateKeyPassphrase is supplied at connect time when the stored
	// private key is encrypted.
	PrivateKeyPassphrase string `json:"private_key_passphrase"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
}

// wsReadConn is the minimal interface needed from *websocket.Conn for
// [readTerminalCredentials]. Defined separately so tests can stub it.
type wsReadConn interface {
	ReadMessage() (int, []byte, error)
	SetReadDeadline(time.Time) error
}

// readTerminalCredentials reads the first text message and returns the
// effective credentials.
//
// When use_stored_credentials is true the target's stored SSH
// username, password, and key are used; the client may still supply
// password or private_key_passphrase to override or complete missing
// fields.
func readTerminalCredentials(conn wsReadConn, target *access.Target) (sshproxy.Credentials, error) {
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	mt, msg, err := conn.ReadMessage()
	if err != nil {
		return sshproxy.Credentials{}, err
	}
	if mt != websocket.TextMessage {
		return sshproxy.Credentials{}, errInvalidCredentials
	}
	var m wsAuthMessage
	if err := json.Unmarshal(msg, &m); err != nil {
		return sshproxy.Credentials{}, errInvalidCredentials
	}
	if m.UseStoredCredentials {
		if target.SSHUsername == "" {
			return sshproxy.Credentials{}, errNoStoredCredentials
		}
		// Passwords / private keys may be absent from the stored
		// record; the client is allowed to supply them at connect time.
		// Connect-time values override any stored value so operators
		// can correct mistakes without re-saving the target.
		password := target.SSHPassword
		if m.Password != "" {
			password = m.Password
		}
		creds := sshproxy.Credentials{
			Username:    target.SSHUsername,
			Password:    password,
			Name:        m.Name,
			Description: m.Description,
		}
		if target.Protocol != access.ProtocolTelnet {
			keyPassphrase := target.SSHPrivateKeyPassphrase
			if m.PrivateKeyPassphrase != "" {
				keyPassphrase = m.PrivateKeyPassphrase
			}
			creds.PrivateKey = target.SSHPrivateKey
			creds.PrivateKeyPassphrase = keyPassphrase
		}
		if err := credentialsDecrypted(creds); err != nil {
			return sshproxy.Credentials{}, err
		}
		return creds, nil
	}
	if m.Username == "" {
		return sshproxy.Credentials{}, errInvalidCredentials
	}
	creds := sshproxy.Credentials{
		Username:    m.Username,
		Password:    m.Password,
		Name:        m.Name,
		Description: m.Description,
	}
	// If the target stores an encrypted private key and the client sent
	// the passphrase, decrypt and use it (SSH only).
	if target.Protocol != access.ProtocolTelnet && target.SSHPrivateKey != "" && m.PrivateKeyPassphrase != "" {
		creds.PrivateKey = target.SSHPrivateKey
		creds.PrivateKeyPassphrase = m.PrivateKeyPassphrase
	}
	if err := credentialsDecrypted(creds); err != nil {
		return sshproxy.Credentials{}, err
	}
	return creds, nil
}

// credentialsDecrypted returns an error if PrivateKey or
// PrivateKeyPassphrase is still ciphertext (decryption failed at rest).
func credentialsDecrypted(creds sshproxy.Credentials) error {
	if creds.PrivateKey != "" && strings.HasPrefix(creds.PrivateKey, secret.CiphertextVersionPrefix) {
		return errCredentialsNotDecrypted
	}
	if creds.PrivateKeyPassphrase != "" && strings.HasPrefix(creds.PrivateKeyPassphrase, secret.CiphertextVersionPrefix) {
		return errCredentialsNotDecrypted
	}
	return nil
}

var (
	errInvalidCredentials  = errors.New("invalid or missing credentials (send JSON: {\"username\":\"...\",\"password\":\"...\"} or {\"use_stored_credentials\":true})")
	errNoStoredCredentials = errors.New("stored credentials not configured for this target")
	// errCredentialsNotDecrypted is shown to the user in Japanese; the
	// HTTP layer never logs the raw error string, so leaking sensitive
	// detail through it is not a concern.
	errCredentialsNotDecrypted = errors.New("保存された認証情報の復号に失敗しています。VANTYX_SSH_PASSWORD_ENCRYPTION_KEY を確認してください")
)
