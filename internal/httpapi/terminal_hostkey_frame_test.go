package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

func TestEncodeHostKeyErrorFrame_Unknown(t *testing.T) {
	target := &access.Target{ID: "t1", Host: "10.0.0.1", Port: 22}
	err := &sshproxy.HostKeyUnknownError{Host: "10.0.0.1", Got: "SHA256:zzz"}

	frame, evt, ok := encodeHostKeyErrorFrame(err, target)
	if !ok {
		t.Fatal("expected ok=true for HostKeyUnknownError")
	}
	if evt != "terminal_host_key_unknown_presented" {
		t.Fatalf("unexpected audit event %q", evt)
	}
	var got hostKeyUnknownFrame
	if err := json.Unmarshal(frame, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != "host_key_unknown" {
		t.Fatalf("type=%q", got.Type)
	}
	if got.TargetID != "t1" || got.Host != "10.0.0.1" || got.Port != 22 || got.Fingerprint != "SHA256:zzz" {
		t.Fatalf("unexpected payload: %+v", got)
	}
}

func TestEncodeHostKeyErrorFrame_Mismatch(t *testing.T) {
	target := &access.Target{ID: "t2", Host: "10.0.0.2", Port: 2222}
	err := &sshproxy.HostKeyMismatchError{Host: "10.0.0.2", Expected: "SHA256:abc", Got: "SHA256:xyz"}

	frame, evt, ok := encodeHostKeyErrorFrame(err, target)
	if !ok {
		t.Fatal("expected ok=true for HostKeyMismatchError")
	}
	if evt != "terminal_host_key_mismatch_presented" {
		t.Fatalf("unexpected audit event %q", evt)
	}
	var got hostKeyMismatchFrame
	if err := json.Unmarshal(frame, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != "host_key_mismatch" {
		t.Fatalf("type=%q", got.Type)
	}
	if got.TargetID != "t2" || got.Host != "10.0.0.2" || got.Port != 2222 {
		t.Fatalf("unexpected payload: %+v", got)
	}
	if got.Expected != "SHA256:abc" || got.Offered != "SHA256:xyz" {
		t.Fatalf("expected/offered mismatch: %+v", got)
	}
}

func TestEncodeHostKeyErrorFrame_WrappedUnknown(t *testing.T) {
	target := &access.Target{ID: "t3", Host: "h", Port: 22}
	err := fmt.Errorf("ssh handshake: %w", &sshproxy.HostKeyUnknownError{Host: "h", Got: "SHA256:fp"})

	if _, _, ok := encodeHostKeyErrorFrame(err, target); !ok {
		t.Fatal("expected ok=true for wrapped HostKeyUnknownError")
	}
}

func TestEncodeHostKeyErrorFrame_OtherError(t *testing.T) {
	target := &access.Target{ID: "t4", Host: "h", Port: 22}
	if frame, evt, ok := encodeHostKeyErrorFrame(errors.New("some other error"), target); ok || frame != nil || evt != "" {
		t.Fatalf("expected ok=false for unrelated error, got ok=%v frame=%q evt=%q", ok, string(frame), evt)
	}
}

func TestEncodeHostKeyErrorFrame_NilInputs(t *testing.T) {
	if _, _, ok := encodeHostKeyErrorFrame(nil, &access.Target{ID: "t"}); ok {
		t.Fatal("expected ok=false for nil error")
	}
	if _, _, ok := encodeHostKeyErrorFrame(&sshproxy.HostKeyUnknownError{}, nil); ok {
		t.Fatal("expected ok=false for nil target")
	}
}
