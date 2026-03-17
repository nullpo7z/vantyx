package session

import (
	"context"
	"testing"
	"time"
)

func TestSession_DoneClosesOnCancel(t *testing.T) {
	m := NewManager()
	id := ID("s1")
	s, err := m.Start(id, StartOptions{UserID: "u", TargetID: "t"}, func(ctx context.Context, sess *Session) {
		<-ctx.Done()
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	go m.Stop(id)
	select {
	case <-s.Done():
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("expected Done() to close")
	}
}
