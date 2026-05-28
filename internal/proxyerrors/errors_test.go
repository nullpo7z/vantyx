package proxyerrors

import (
	"errors"
	"net"
	"strings"
	"testing"
)

func TestWrapTCPDialError_Timeout(t *testing.T) {
	err := WrapTCPDialError("SSH", errors.New("dial tcp 10.0.0.1:22: i/o timeout"))
	var ufe *UserFacingError
	if !errors.As(err, &ufe) {
		t.Fatal("expected UserFacingError")
	}
	if ufe.Message == "" || ufe.Message == ufe.Err.Error() {
		t.Fatalf("expected hint message, got %q", ufe.Message)
	}
	if !strings.Contains(ufe.Message, "SSH") {
		t.Fatalf("expected proto in message, got %q", ufe.Message)
	}
}

func TestWrapTCPDialError_Refused(t *testing.T) {
	err := WrapTCPDialError("Telnet", errors.New("dial tcp 10.0.0.1:23: connect: connection refused"))
	var ufe *UserFacingError
	if !errors.As(err, &ufe) {
		t.Fatal("expected UserFacingError")
	}
	if !strings.Contains(ufe.Message, "refused") {
		t.Fatalf("expected refused hint, got %q", ufe.Message)
	}
}

func TestWrapTCPDialError_NetTimeout(t *testing.T) {
	err := WrapTCPDialError("SSH", &net.OpError{Op: "dial", Err: &timeoutErr{}})
	var ufe *UserFacingError
	if !errors.As(err, &ufe) {
		t.Fatal("expected UserFacingError")
	}
}

func TestBridgeErrorMessage(t *testing.T) {
	err := WrapTCPDialError("SSH", errors.New("dial tcp: i/o timeout"))
	if !strings.Contains(BridgeErrorMessage(err), "SSH") {
		t.Fatalf("BridgeErrorMessage: %q", BridgeErrorMessage(err))
	}
}

func TestUnwrapForAudit(t *testing.T) {
	inner := errors.New("dial tcp: i/o timeout")
	err := WrapTCPDialError("Telnet", inner)
	if UnwrapForAudit(err) != inner.Error() {
		t.Fatalf("UnwrapForAudit: %q", UnwrapForAudit(err))
	}
}

func TestBridgeErrorKey_CoversAllBranches(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		proto   string
		wantKey string
		// wantVars lists the (name, value) pairs the test expects to find
		// in the returned Vars slice; ordering is checked positionally.
		wantVars []any
	}{
		{
			name:     "i/o timeout",
			err:      errors.New("dial tcp 10.0.0.1:22: i/o timeout"),
			proto:    "SSH",
			wantKey:  "proxy.tcpTimeout",
			wantVars: []any{"proto", "SSH"},
		},
		{
			name:     "connection refused",
			err:      errors.New("dial tcp 10.0.0.1:23: connect: connection refused"),
			proto:    "Telnet",
			wantKey:  "proxy.connectionRefused",
			wantVars: []any{"proto", "Telnet"},
		},
		{
			name:     "no route to host",
			err:      errors.New("dial tcp 10.0.0.1: connect: no route to host"),
			proto:    "SSH",
			wantKey:  "proxy.noRoute",
			wantVars: []any{"proto", "SSH"},
		},
		{
			name:    "network unreachable",
			err:     errors.New("dial tcp: connect: network is unreachable"),
			proto:   "SSH",
			wantKey: "proxy.networkUnreachable",
		},
		{
			name:     "dial failed (catch-all)",
			err:      errors.New("some other dial error"),
			proto:    "SSH",
			wantKey:  "proxy.dialFailed",
			wantVars: []any{"proto", "SSH"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := WrapTCPDialError(tc.proto, tc.err)
			key, vars, ok := BridgeErrorKey(got)
			if !ok {
				t.Fatalf("BridgeErrorKey: ok=false, want true for %v", got)
			}
			if key != tc.wantKey {
				t.Fatalf("key: got %q, want %q", key, tc.wantKey)
			}
			if len(vars) != len(tc.wantVars) {
				t.Fatalf("vars: got %v, want %v", vars, tc.wantVars)
			}
			for i := range vars {
				if vars[i] != tc.wantVars[i] {
					t.Fatalf("vars[%d]: got %v, want %v", i, vars[i], tc.wantVars[i])
				}
			}
		})
	}
}

func TestBridgeErrorKey_NotUserFacing(t *testing.T) {
	if _, _, ok := BridgeErrorKey(errors.New("plain")); ok {
		t.Fatal("expected ok=false for non-UserFacingError")
	}
	if _, _, ok := BridgeErrorKey(nil); ok {
		t.Fatal("expected ok=false for nil error")
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
