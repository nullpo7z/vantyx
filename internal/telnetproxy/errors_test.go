package telnetproxy

import (
	"errors"
	"net"
	"strings"
	"testing"
)

func TestWrapDialError_Timeout(t *testing.T) {
	err := WrapDialError(errors.New("dial tcp 10.0.0.1:23: i/o timeout"))
	var ufe *UserFacingError
	if !errors.As(err, &ufe) {
		t.Fatal("expected UserFacingError")
	}
	if ufe.Message == "" || ufe.Message == ufe.Err.Error() {
		t.Fatalf("expected hint message, got %q", ufe.Message)
	}
}

func TestWrapDialError_Refused(t *testing.T) {
	err := WrapDialError(errors.New("dial tcp 10.0.0.1:23: connect: connection refused"))
	var ufe *UserFacingError
	if !errors.As(err, &ufe) {
		t.Fatal("expected UserFacingError")
	}
	if !strings.Contains(ufe.Message, "refused") {
		t.Fatalf("expected refused hint, got %q", ufe.Message)
	}
}

func TestWrapDialError_NetTimeout(t *testing.T) {
	err := WrapDialError(&net.OpError{Op: "dial", Err: &timeoutErr{}})
	var ufe *UserFacingError
	if !errors.As(err, &ufe) {
		t.Fatal("expected UserFacingError")
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
