package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/websocket"
)

func TestHandleSSHWebSocket_UnauthorizedWithoutCookie(t *testing.T) {
	app := NewApp()
	router := app.NewRouter()

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh"}

	_, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err == nil {
		t.Fatalf("expected WebSocket dial to fail without cookie, got nil error")
	}
}

func TestHandleSSHWebSocket_EchoRoundTrip(t *testing.T) {
	app := NewApp()
	router := app.NewRouter()

	// Create an authenticated HTTP cookie session.
	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session returned error: %v", err)
	}

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh"}

	header := http.Header{}
	header.Add("Cookie", (&http.Cookie{
		Name:  "vantyx_session",
		Value: httpSess.ID,
		Path:  "/",
	}).String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	want := "hello-terminal"
	if err := conn.WriteMessage(websocket.TextMessage, []byte(want)); err != nil {
		t.Fatalf("WriteMessage failed: %v", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}
	if string(msg) != want {
		t.Fatalf("expected echo %q, got %q", want, string(msg))
	}
}
