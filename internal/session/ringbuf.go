package session

import (
	"io"
	"sync"
)

// DefaultRingBufferSize is the default capacity in bytes for terminal output replay.
const DefaultRingBufferSize = 256 * 1024 // 256 KiB

// RingBuffer is a fixed-size circular buffer for terminal stdout/stderr.
// It is safe for concurrent use and supports replay for session resume.
type RingBuffer struct {
	mu   sync.RWMutex
	buf  []byte
	cap  int
	head int // next write position
	size int // current length
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultRingBufferSize
	}
	return &RingBuffer{
		buf: make([]byte, capacity),
		cap: capacity,
	}
}

// Write appends p to the buffer. Oldest data is overwritten if the buffer is full.
func (r *RingBuffer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n = len(p)
	if n >= r.cap {
		// Keep only the last r.cap bytes of p
		p = p[len(p)-r.cap:]
		copy(r.buf, p)
		r.head = 0
		r.size = r.cap
		return n, nil
	}
	for _, b := range p {
		r.buf[r.head] = b
		r.head = (r.head + 1) % r.cap
		if r.size < r.cap {
			r.size++
		}
	}
	return n, nil
}

// Bytes returns a copy of the buffer contents in chronological order (oldest first).
func (r *RingBuffer) Bytes() []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.size == 0 {
		return nil
	}
	out := make([]byte, r.size)
	if r.size < r.cap {
		copy(out, r.buf[:r.size])
		return out
	}
	// Buffer is full: head is the oldest position
	copy(out, r.buf[r.head:])
	copy(out[r.cap-r.head:], r.buf[:r.head])
	return out
}

// Len returns the number of bytes currently in the buffer.
func (r *RingBuffer) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.size
}

// Tee returns an io.Writer that writes to w and also to this RingBuffer.
func (r *RingBuffer) Tee(w io.Writer) io.Writer {
	return &teeWriter{w: w, buf: r}
}

type teeWriter struct {
	w   io.Writer
	buf *RingBuffer
}

func (t *teeWriter) Write(p []byte) (n int, err error) {
	n, err = t.w.Write(p)
	if n > 0 {
		_, _ = t.buf.Write(p[:n])
	}
	return n, err
}
