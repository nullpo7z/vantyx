package telnetproxy

import (
	"errors"
	"net"
	"os"
	"strings"
)

// UserFacingError wraps a dial/bridge error with a short Japanese hint for terminal UI.
type UserFacingError struct {
	Err     error
	Message string
}

func (e *UserFacingError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Err.Error()
}

func (e *UserFacingError) Unwrap() error { return e.Err }

// WrapDialError returns a user-facing error for Telnet TCP dial failures.
func WrapDialError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "i/o timeout") || strings.Contains(lower, "timeout"):
		return &UserFacingError{
			Err: err,
			Message: "Telnet への TCP 接続がタイムアウトしました。機器で Telnet が有効か、ファイアウォール・ACL・ポート番号、および Vantyx からのネットワーク経路を確認してください。",
		}
	case strings.Contains(lower, "connection refused"):
		return &UserFacingError{
			Err: err,
			Message: "Telnet ポートへの接続が拒否されました。サービスが起動しているか、正しいポート番号かを確認してください。",
		}
	case strings.Contains(lower, "no route to host"):
		return &UserFacingError{
			Err: err,
			Message: "Telnet 先へのルートがありません。IP アドレス・VLAN・ルーティングを確認してください。",
		}
	case strings.Contains(lower, "network is unreachable"):
		return &UserFacingError{
			Err: err,
			Message: "ネットワークに到達できません。Vantyx サーバからターゲットへの経路を確認してください。",
		}
	default:
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return &UserFacingError{
				Err: err,
				Message: "Telnet への TCP 接続がタイムアウトしました。機器で Telnet が有効か、ファイアウォール・ACL・ポート番号、および Vantyx からのネットワーク経路を確認してください。",
			}
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return &UserFacingError{
				Err: err,
				Message: "Telnet への TCP 接続がタイムアウトしました。機器で Telnet が有効か、ファイアウォール・ACL・ポート番号、および Vantyx からのネットワーク経路を確認してください。",
			}
		}
		return &UserFacingError{
			Err: err,
			Message: "Telnet 接続に失敗しました: " + msg + " （ホスト・ポート・Telnet 有効化を確認してください）",
		}
	}
}
