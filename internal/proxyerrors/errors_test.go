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

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
