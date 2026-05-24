package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSessionEventBroker_Broadcast(t *testing.T) {
	b := NewSessionEventBroker()
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)
	b.Broadcast()
	select {
	case msg := <-ch:
		if !strings.Contains(string(msg), "session_change") {
			t.Fatalf("msg=%s", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for broadcast")
	}
}

func TestSessionEventBroker_BroadcastSkipsSlowClient(t *testing.T) {
	b := NewSessionEventBroker()
	slow := b.Subscribe()
	for i := 0; i < 8; i++ {
		slow <- []byte("fill")
	}
	fast := b.Subscribe()
	defer b.Unsubscribe(fast)
	b.Broadcast()
	select {
	case <-fast:
	case <-time.After(time.Second):
		t.Fatal("fast client should receive broadcast")
	}
}

func TestHandleSessionEvents_NoBroker(t *testing.T) {
	app := newTestApp(t)
	app.SessionEventBroker = nil
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/events/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("got %d", w.Result().StatusCode)
	}
}

func TestHandleSessionEvents_Unauthorized(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/events/sessions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("got %d", w.Result().StatusCode)
	}
}

func TestHandleSessionEvents_ReceivesBroadcast(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events/sessions", nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		router.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	app.SessionEventBroker.Broadcast()
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit")
	}
	if body := w.Body.String(); !strings.Contains(body, "session_change") {
		t.Fatalf("body=%q", body)
	}
}
