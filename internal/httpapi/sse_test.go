package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/filetransfer"
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

func TestFileTransferEventBroker_Publish(t *testing.T) {
	b := NewFileTransferEventBroker()
	ch := b.Subscribe("user1")
	defer b.Unsubscribe(ch)

	snap := filetransfer.JobSnapshot{ID: "j1", State: "running", Progress: 100, Total: 1000}
	b.Publish(snap, "user1")

	select {
	case msg := <-ch:
		if !strings.Contains(string(msg), `"id":"j1"`) {
			t.Fatalf("msg=%s", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for publish")
	}
}

func TestFileTransferEventBroker_FiltersByUser(t *testing.T) {
	b := NewFileTransferEventBroker()
	chA := b.Subscribe("alice")
	chB := b.Subscribe("bob")
	defer b.Unsubscribe(chA)
	defer b.Unsubscribe(chB)

	snap := filetransfer.JobSnapshot{ID: "secret", State: "running"}
	b.Publish(snap, "alice")

	select {
	case <-chA:
	case <-time.After(time.Second):
		t.Fatal("alice should receive own events")
	}
	select {
	case msg := <-chB:
		t.Fatalf("bob must not receive alice events: %s", msg)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestFileTransferEventBroker_ProgressThrottle(t *testing.T) {
	b := NewFileTransferEventBroker()
	ch := b.Subscribe("user1")
	defer b.Unsubscribe(ch)

	// State change always passes.
	b.Publish(filetransfer.JobSnapshot{ID: "j", State: "running", Progress: 1}, "user1")
	// Immediate second progress with same state is throttled.
	b.Publish(filetransfer.JobSnapshot{ID: "j", State: "running", Progress: 2}, "user1")

	// First event arrives.
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("first event missing")
	}
	// Second is dropped due to throttle.
	select {
	case msg := <-ch:
		t.Fatalf("expected throttle to drop event, got %s", msg)
	case <-time.After(50 * time.Millisecond):
	}

	// State transition bypasses throttle.
	b.Publish(filetransfer.JobSnapshot{ID: "j", State: "completed", Progress: 2}, "user1")
	select {
	case msg := <-ch:
		if !strings.Contains(string(msg), `"state":"completed"`) {
			t.Fatalf("expected completed event, got %s", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("state-change event should pass throttle")
	}
}

func TestFileTransferEventBroker_DropsSlowSubscriber(t *testing.T) {
	b := NewFileTransferEventBroker()
	slow := b.Subscribe("user1")
	// fill the buffer (capacity 32)
	for i := 0; i < 40; i++ {
		// Use different states so shouldEmit always lets through.
		state := "running"
		if i%2 == 0 {
			state = "receiving"
		}
		b.Publish(filetransfer.JobSnapshot{ID: "fill", State: state}, "user1")
	}
	// drain
	drained := 0
	for {
		select {
		case <-slow:
			drained++
		default:
			b.Unsubscribe(slow)
			if drained == 0 {
				t.Fatal("should have drained at least one event")
			}
			return
		}
	}
}

func TestHandleFileTransferEvents_NoBroker(t *testing.T) {
	app := newTestApp(t)
	app.FileTransferEventBroker = nil
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/events/file-transfers", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("got %d", w.Result().StatusCode)
	}
}

func TestHandleFileTransferEvents_Unauthorized(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/events/file-transfers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("got %d", w.Result().StatusCode)
	}
}

func TestHandleFileTransferEvents_ReceivesPublish(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events/file-transfers", nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		router.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	app.FileTransferEventBroker.Publish(
		filetransfer.JobSnapshot{ID: "live-job", State: "running"},
		"admin",
	)
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit")
	}
	if body := w.Body.String(); !strings.Contains(body, "live-job") {
		t.Fatalf("body=%q", body)
	}
}

func TestHandleFileTransferEvents_SendsInitialSnapshot(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")

	// Pre-create a job for the user so the initial snapshot dump emits it.
	_, err := app.FileTransferManager.Create(filetransfer.CreateOpts{
		UserID:       "admin",
		TargetID:     "t1",
		TargetName:   "T1",
		Backend:      filetransfer.BackendRemote,
		Direction:    filetransfer.DirectionDownload,
		RemotePath:   "/a.txt",
		FileName:     "a.txt",
		InitialState: filetransfer.StateRunning,
	}, func() {})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events/file-transfers", nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		router.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit")
	}
	body := w.Body.String()
	if !strings.Contains(body, `"file_name":"a.txt"`) {
		t.Fatalf("expected initial snapshot in body, got: %s", body)
	}
}
