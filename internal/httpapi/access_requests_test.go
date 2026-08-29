package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
)

func TestApp_AccessRequestWorkflow(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	if _, err := app.UserStore.CreateUser("bob", "bob", "Password1!", "user"); err != nil {
		t.Fatal(err)
	}
	for _, g := range []string{"net", "net/tokyo", "ops"} {
		if _, err := app.AccessGroupStore.Create(ctx, access.GroupID(g), g); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := app.TargetStore.CreateWithPath(ctx, "t1", "t1", "127.0.0.1", 22, access.ProtocolSSH, "net/tokyo", "net/tokyo", "", "", "", "", true, false, false); err != nil {
		t.Fatal(err)
	}
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, "net/tokyo", "t1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, "bob", "ops")
	adminSess, _ := app.SessionStore.Create("admin")
	bobSess, _ := app.SessionStore.Create("bob")

	do := func(sessID, method, path string, body interface{}) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, jsonReq(t, method, path, body, sessID))
		return w
	}

	// Requestable groups exclude what bob already has.
	w := do(bobSess.ID, http.MethodGet, "/api/access-requests/groups", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"net","name":"net","has_access":false`) || !strings.Contains(w.Body.String(), `"id":"ops","name":"ops","has_access":true`) {
		t.Fatalf("groups: %d %s", w.Code, w.Body.String())
	}

	// Validation.
	if w := do(bobSess.ID, http.MethodPost, "/api/access-requests", map[string]interface{}{"group_id": "nope"}); w.Code != http.StatusNotFound {
		t.Fatalf("unknown group: %d", w.Code)
	}
	if w := do(bobSess.ID, http.MethodPost, "/api/access-requests", map[string]interface{}{"group_id": "ops"}); w.Code != http.StatusConflict {
		t.Fatalf("already member: %d", w.Code)
	}
	if w := do(bobSess.ID, http.MethodPost, "/api/access-requests", map[string]interface{}{"group_id": "net", "duration_seconds": -1}); w.Code != http.StatusBadRequest {
		t.Fatalf("negative duration: %d", w.Code)
	}
	if w := do("", http.MethodPost, "/api/access-requests", map[string]interface{}{"group_id": "net"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", w.Code)
	}

	// Create.
	w = do(bobSess.ID, http.MethodPost, "/api/access-requests", map[string]interface{}{"group_id": "net", "reason": "oncall", "duration_seconds": 3600})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := decodeJSON(t, w)
	id, _ := created["id"].(string)
	if id == "" || created["status"] != "pending" || created["username"] != "bob" {
		t.Fatalf("created = %v", created)
	}
	if w := do(bobSess.ID, http.MethodPost, "/api/access-requests", map[string]interface{}{"group_id": "net"}); w.Code != http.StatusConflict {
		t.Fatalf("duplicate pending: %d", w.Code)
	}
	if ok, _ := app.userCanAccessTarget(ctx, "bob", "t1"); ok {
		t.Fatal("access granted before approval")
	}

	// Only admins decide; bob sees only his own list.
	if w := do(bobSess.ID, http.MethodPost, "/api/access-requests/"+id+"/approve", nil); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin approve: %d", w.Code)
	}
	w = do(bobSess.ID, http.MethodGet, "/api/access-requests", nil)
	var mine []map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &mine)
	if len(mine) != 1 || mine[0]["id"] != id {
		t.Fatalf("bob's list = %s", w.Body.String())
	}
	w = do(adminSess.ID, http.MethodGet, "/api/access-requests?status=pending", nil)
	if !strings.Contains(w.Body.String(), id) {
		t.Fatalf("admin pending list: %s", w.Body.String())
	}

	// Approve with an overridden duration -> membership with expiry,
	// inherited access to net/tokyo's target.
	w = do(adminSess.ID, http.MethodPost, "/api/access-requests/"+id+"/approve", map[string]interface{}{"duration_seconds": 7200, "note": "ok for today"})
	if w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	approved := decodeJSON(t, w)
	if approved["status"] != "approved" || approved["decided_by"] != "admin" || approved["decision_note"] != "ok for today" || approved["expires_at"] == nil {
		t.Fatalf("approved = %v", approved)
	}
	if ok, _ := app.userCanAccessTarget(ctx, "bob", "t1"); !ok {
		t.Fatal("no access after approval")
	}
	rows, _ := app.AccessGroupStore.MembershipsForGroup(ctx, "net")
	if len(rows) != 1 || rows[0].UserID != "bob" || rows[0].ExpiresAt == nil {
		t.Fatalf("memberships = %+v", rows)
	}
	// Deciding twice is refused.
	if w := do(adminSess.ID, http.MethodPost, "/api/access-requests/"+id+"/deny", nil); w.Code != http.StatusConflict {
		t.Fatalf("decide twice: %d", w.Code)
	}

	// Deny path and withdraw path.
	w = do(bobSess.ID, http.MethodPost, "/api/access-requests", map[string]interface{}{"group_id": "net/tokyo"})
	if w.Code != http.StatusConflict {
		t.Fatalf("net/tokyo is inherited now, want 409: %d", w.Code)
	}
	if _, err := app.AccessGroupStore.Create(ctx, "lab", "lab"); err != nil {
		t.Fatal(err)
	}
	w = do(bobSess.ID, http.MethodPost, "/api/access-requests", map[string]interface{}{"group_id": "lab"})
	id2 := decodeJSON(t, w)["id"].(string)
	if w := do(adminSess.ID, http.MethodPost, "/api/access-requests/"+id2+"/deny", map[string]interface{}{"note": "no"}); w.Code != http.StatusOK || decodeJSON(t, w)["status"] != "denied" {
		t.Fatalf("deny: %d %s", w.Code, w.Body.String())
	}
	if ok, _ := app.userCanAccessTarget(ctx, "bob", "t1"); !ok {
		t.Fatal("deny of another request must not touch existing access")
	}
	w = do(bobSess.ID, http.MethodPost, "/api/access-requests", map[string]interface{}{"group_id": "lab"})
	id3 := decodeJSON(t, w)["id"].(string)
	if _, err := app.UserStore.CreateUser("carol", "carol", "Password1!", "user"); err != nil {
		t.Fatal(err)
	}
	carolSess, _ := app.SessionStore.Create("carol")
	if w := do(carolSess.ID, http.MethodDelete, "/api/access-requests/"+id3, nil); w.Code != http.StatusForbidden {
		t.Fatalf("withdraw someone else's: %d", w.Code)
	}
	if w := do(bobSess.ID, http.MethodDelete, "/api/access-requests/"+id3, nil); w.Code != http.StatusNoContent {
		t.Fatalf("withdraw: %d", w.Code)
	}
	if n, _ := app.AccessRequests.CountPending(ctx); n != 0 {
		t.Fatalf("pending count = %d", n)
	}
}
