package session

import (
	"bytes"
	"testing"
)

func TestRingBuffer_WriteAndBytes(t *testing.T) {
	r := NewRingBuffer(16)
	n, err := r.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 5 {
		t.Fatalf("Write returned %d, want 5", n)
	}
	got := r.Bytes()
	if !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("Bytes() = %q, want hello", got)
	}
	if r.Len() != 5 {
		t.Fatalf("Len() = %d, want 5", r.Len())
	}
}

func TestRingBuffer_Overflow(t *testing.T) {
	r := NewRingBuffer(5)
	_, _ = r.Write([]byte("abcdef")) // 6 bytes into cap 5
	got := r.Bytes()
	want := []byte("bcdef") // last 5
	if !bytes.Equal(got, want) {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}
}

func TestRingBuffer_Tee(t *testing.T) {
	r := NewRingBuffer(8)
	var buf bytes.Buffer
	w := r.Tee(&buf)
	_, _ = w.Write([]byte("hi"))
	_, _ = w.Write([]byte("!"))
	if g := buf.String(); g != "hi!" {
		t.Fatalf("tee writer got %q", g)
	}
	if g := string(r.Bytes()); g != "hi!" {
		t.Fatalf("ring buffer got %q", g)
	}
}
