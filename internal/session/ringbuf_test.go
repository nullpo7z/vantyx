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

func TestRingBuffer_ZeroOrNegativeCapacity(t *testing.T) {
	r := NewRingBuffer(0)
	if r == nil || r.cap != DefaultRingBufferSize {
		t.Fatalf("capacity 0 should use default size")
	}
	r2 := NewRingBuffer(-1)
	if r2 == nil || r2.cap != DefaultRingBufferSize {
		t.Fatalf("capacity -1 should use default size")
	}
}

func TestRingBuffer_WriteEmpty(t *testing.T) {
	r := NewRingBuffer(8)
	n, err := r.Write(nil)
	if n != 0 || err != nil {
		t.Fatalf("Write(nil): n=%d err=%v", n, err)
	}
	n, err = r.Write([]byte{})
	if n != 0 || err != nil {
		t.Fatalf("Write([]): n=%d err=%v", n, err)
	}
	if r.Len() != 0 {
		t.Fatalf("expected len 0, got %d", r.Len())
	}
}

func TestRingBuffer_BytesWhenFull(t *testing.T) {
	r := NewRingBuffer(4)
	_, _ = r.Write([]byte("abcd"))
	got := r.Bytes()
	if string(got) != "abcd" {
		t.Fatalf("Bytes() = %q", got)
	}
	// overflow so head wraps
	_, _ = r.Write([]byte("ef"))
	got = r.Bytes()
	if string(got) != "cdef" {
		t.Fatalf("after overflow Bytes() = %q, want cdef", got)
	}
}
