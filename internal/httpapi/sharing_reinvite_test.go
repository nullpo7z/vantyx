package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestKickThenNamedReinvite_LiftsBlockAndJoins is the F-2 / E-16
// acceptance test: after the owner removes a participant, the *only* way
// back in is a new named invitation from the owner. Issuing one lifts the
// block (audited as session_participant_unkicked) and the invitee can
// join with it; the participants list exposes the block in between.
func TestKickThenNamedReinvite_LiftsBlockAndJoins(t *testing.T) {
	app, sid, _, bob := setupSharingTerminalFixtures(t)
	room := app.SharingRegistry.EnsureRoom(string(sid), "demo", "admin", "admin")
	if err := room.AddViewer(bob, "bob", "inv-0", time.Now().UTC()); err != nil {
		t.Fatalf("AddViewer: %v", err)
	}
	router := app.NewRouter()
	adminSess, _ := app.SessionStore.Create("admin")
	bobSess, _ := app.SessionStore.Create(bob)
	base := "/api/terminal/sessions/" + string(sid)
	do := func(sessID, method, path string, body []byte) *httptest.ResponseRecorder {
		var req *http.Request
		if body != nil {
			req = httptest.NewRequest(method, path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// Owner removes bob.
	if w := do(adminSess.ID, http.MethodDelete, base+"/participants/"+bob, nil); w.Code != http.StatusNoContent {
		t.Fatalf("kick: status=%d body=%s", w.Code, w.Body.String())
	}
	if !room.IsKicked(bob) {
		t.Fatal("expected bob to be blocked after the kick")
	}
	var list struct {
		Kicked []string `json:"kicked"`
	}
	w := do(adminSess.ID, http.MethodGet, base+"/participants", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Kicked) != 1 || list.Kicked[0] != bob {
		t.Fatalf("participants list should expose the block, got kicked=%v", list.Kicked)
	}

	// Owner issues a new named invitation to bob: this lifts the block.
	createBody, _ := json.Marshal(map[string]interface{}{"mode": "viewer", "invitee_user_id": bob})
	w = do(adminSess.ID, http.MethodPost, base+"/invitations", createBody)
	if w.Code != http.StatusOK {
		t.Fatalf("re-invite: status=%d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil || created.ID == "" {
		t.Fatalf("decode invitation: err=%v id=%q", err, created.ID)
	}
	if room.IsKicked(bob) {
		t.Fatal("a named re-invitation must lift the kick block")
	}
	if e, ok := latestAuditEvent("session_participant_unkicked"); !ok || e.Fields["target_id"] != bob {
		t.Fatalf("expected session_participant_unkicked audit for %s, got ok=%v fields=%v", bob, ok, e.Fields)
	}

	// bob joins with the new invitation: allowed, and the invitation is
	// consumed by a *successful* join (not burned by a refused one).
	joinBody, _ := json.Marshal(map[string]string{"invitation_id": created.ID})
	if w := do(bobSess.ID, http.MethodPost, base+"/join", joinBody); w.Code != http.StatusOK {
		t.Fatalf("join after re-invite: status=%d body=%s", w.Code, w.Body.String())
	}
	if !room.IsParticipant(bob) {
		t.Fatal("bob should be a participant again after joining")
	}
	w = do(adminSess.ID, http.MethodGet, base+"/participants", nil)
	list.Kicked = nil
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Kicked) != 0 {
		t.Fatalf("no one should be listed as kicked after the rejoin, got %v", list.Kicked)
	}

	// The removed allow-rejoin endpoint must be gone.
	if w := do(adminSess.ID, http.MethodPost, base+"/participants/"+bob+"/allow-rejoin", nil); w.Code == http.StatusNoContent {
		t.Fatal("allow-rejoin endpoint should no longer exist")
	}
}
