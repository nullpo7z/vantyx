package telnetproxy

import "bytes"

// LoginAutomater sends username/password when common Telnet login prompts appear in server output.
type LoginAutomater struct {
	username string
	password string
	state    int
	tail     []byte
}

// NewLoginAutomater returns an automater, or nil if username is empty.
func NewLoginAutomater(username, password string) *LoginAutomater {
	if username == "" {
		return nil
	}
	return &LoginAutomater{username: username, password: password}
}

// OnOutput inspects server text and writes credentials to the Telnet connection when prompted.
func (l *LoginAutomater) OnOutput(chunk []byte, write func([]byte) error) {
	if l == nil || l.state >= 2 || len(chunk) == 0 {
		return
	}
	l.tail = append(l.tail, chunk...)
	if len(l.tail) > 512 {
		l.tail = append([]byte(nil), l.tail[len(l.tail)-512:]...)
	}
	lower := bytes.ToLower(l.tail)
	switch l.state {
	case 0:
		if loginPromptSeen(lower) {
			_ = write(append([]byte(l.username), '\r'))
			l.state = 1
			l.tail = l.tail[:0]
		}
	case 1:
		if passwordPromptSeen(lower) {
			_ = write(append([]byte(l.password), '\r'))
			l.state = 2
			l.tail = l.tail[:0]
		}
	}
}

func loginPromptSeen(lower []byte) bool {
	return bytes.Contains(lower, []byte("login:")) ||
		bytes.Contains(lower, []byte("log in:")) ||
		bytes.Contains(lower, []byte("username:")) ||
		bytes.Contains(lower, []byte("user name:")) ||
		bytes.Contains(lower, []byte("user:")) ||
		bytes.Contains(lower, []byte("account:")) ||
		bytes.Contains(lower, []byte("sername:")) ||
		bytes.Contains(lower, []byte("ログイン")) ||
		bytes.Contains(lower, []byte("ユーザ"))
}

func passwordPromptSeen(lower []byte) bool {
	return bytes.Contains(lower, []byte("password:")) ||
		bytes.Contains(lower, []byte("passwort:")) ||
		bytes.Contains(lower, []byte("passwd:")) ||
		bytes.Contains(lower, []byte("パスワード"))
}
