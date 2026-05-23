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
	suffix := promptLineSuffix(lower)
	switch l.state {
	case 0:
		if loginPromptSeen(suffix) {
			_ = write(append([]byte(l.username), '\r'))
			l.state = 1
			l.tail = l.tail[:0]
		}
	case 1:
		if passwordPromptSeen(suffix) {
			_ = write(append([]byte(l.password), '\r'))
			l.state = 2
			l.tail = l.tail[:0]
		}
	}
}

// promptLineSuffix returns the current line tail (after last CR/LF) for prompt matching.
func promptLineSuffix(lower []byte) []byte {
	idx := bytes.LastIndexAny(lower, "\r\n")
	if idx >= 0 && idx+1 < len(lower) {
		return lower[idx+1:]
	}
	return lower
}

func loginPromptSeen(suffix []byte) bool {
	s := bytes.TrimSpace(suffix)
	return bytes.Contains(s, []byte("login:")) ||
		bytes.Contains(s, []byte("log in:")) ||
		bytes.Contains(s, []byte("username:")) ||
		bytes.Contains(s, []byte("user name:")) ||
		bytes.Contains(s, []byte("account:")) ||
		bytes.Contains(s, []byte("account name:")) ||
		bytes.Contains(s, []byte("user access verification")) ||
		bytes.Contains(s, []byte("ログイン")) ||
		bytes.Contains(s, []byte("ユーザ"))
}

func passwordPromptSeen(suffix []byte) bool {
	s := bytes.TrimSpace(suffix)
	return bytes.Contains(s, []byte("password:")) ||
		bytes.Contains(s, []byte("passwort:")) ||
		bytes.Contains(s, []byte("passwd:")) ||
		bytes.Contains(s, []byte("パスワード"))
}
