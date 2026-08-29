package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAllowRejoin_LiftsKickBlock covers the F-1 / E-16 owner control:
// after a kick the participants list exposes the user under "kicked",
// POST .../allow-rejoin lifts the block (204), and the list no longer
// reports them as kicked.
func TestAllowRejoin_LiftsKickBlock(t *testing.T) {
	app, sid, _, bob := setupSharingTerminalFixtures(t)
	room := app.SharingRegistry.EnsureRoom(string(sid), "demo", "admin", "admin")
	if err := room.AddViewer(bob, "bob", "inv-1", time.Now().UTC()); err != nil {
		t.Fatalf("AddViewer: %v", err)
	}
	router := app.NewRouter()
	adminSess, _ := app.SessionStore.Create("admin")
	do := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	base := "/api/terminal/sessions/" + string(sid)

	if w := do(http.MethodDelete, base+"/participants/"+bob); w.Code != http.StatusNoContent {
		t.Fatalf("kick: status=%d body=%s", w.Code, w.Body.String())
	}
	var list struct {
		Kicked []string `json:"kicked"`
	}
	w := do(http.MethodGet, base+"/participants")
	if w.Code != http.StatusOK {
		t.Fatalf("list: status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Kicked) != 1 || list.Kicked[0] != bob {
		t.Fatalf("expected kicked=[%s] after kick, got %v", bob, list.Kicked)
	}

	if w := do(http.MethodPost, base+"/participants/"+bob+"/allow-rejoin"); w.Code != http.StatusNoContent {
		t.Fatalf("allow-rejoin: status=%d body=%s", w.Code, w.Body.String())
	}
	if room.IsKicked(bob) {
		t.Fatal("expected the kick block to be lifted")
	}
	w = do(http.MethodGet, base+"/participants")
	list.Kicked = nil
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Kicked) != 0 {
		t.Fatalf("expected no kicked users after allow-rejoin, got %v", list.Kicked)
	}
	// Allowing again is a no-op reported as not found.
	if w := do(http.MethodPost, base+"/participants/"+bob+"/allow-rejoin"); w.Code != http.StatusNotFound {
		t.Fatalf("second allow-rejoin: expected 404, got %d", w.Code)
	}
	e, ok := latestAuditEvent("session_participant_unkicked")
	if !ok || e.Fields["target_id"] != bob {
		t.Fatalf("expected session_participant_unkicked audit for %s, got ok=%v fields=%v", bob, ok, e.Fields)
	}
}
