package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
)

// seedAdminVisibilityFixtures creates one group with one target and two
// extra users who are members of nothing: a second admin and an ordinary
// user. The built-in "admin" is deliberately NOT added to the group either,
// so nothing in these tests passes because of membership.
func seedAdminVisibilityFixtures(t *testing.T, app *App) {
	t.Helper()
	ctx := context.Background()
	if _, err := app.AccessGroupStore.Create(ctx, access.GroupID("ops"), "Ops"); err != nil {
		t.Fatalf("Create group: %v", err)
	}
	if _, err := app.TargetStore.CreateWithPath(ctx, access.TargetID("srv1"), "Server 1", "10.0.0.1", 22, access.ProtocolSSH, access.GroupID("ops"), "ops", "", "", "", "", true, false, false); err != nil {
		t.Fatalf("CreateWithPath: %v", err)
	}
	if err := app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("ops"), access.TargetID("srv1")); err != nil {
		t.Fatalf("AddTargetToGroup: %v", err)
	}
	if _, err := app.UserStore.CreateUser("admin2", "admin2", "Admin2Pass1!", "admin"); err != nil {
		t.Fatalf("CreateUser admin2: %v", err)
	}
	if _, err := app.UserStore.CreateUser("bob", "bob", "BobPass1!", ""); err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}
}

func getJSONAs(t *testing.T, router http.Handler, app *App, userID, path string) []byte {
	t.Helper()
	sess, _ := app.SessionStore.Create(userID)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s as %s: status=%d body=%s", path, userID, w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

// TestApp_Groups_AdminSeesAllWithoutMembership is the E-10 regression
// guard: a second admin who is not a member of any group must still see
// every group and every target in it via GET /api/groups (the endpoint
// that drives Server management), while an ordinary non-member user sees
// nothing.
func TestApp_Groups_AdminSeesAllWithoutMembership(t *testing.T) {
	app := newTestApp(t)
	seedAdminVisibilityFixtures(t, app)
	router := app.NewRouter()

	var adminGroups []struct {
		ID      string `json:"id"`
		Targets []struct {
			ID string `json:"id"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(getJSONAs(t, router, app, "admin2", "/api/groups"), &adminGroups); err != nil {
		t.Fatalf("decode admin2 groups: %v", err)
	}
	if len(adminGroups) != 1 || adminGroups[0].ID != "ops" {
		t.Fatalf("admin2 should see the ops group without membership, got %+v", adminGroups)
	}
	if len(adminGroups[0].Targets) != 1 || adminGroups[0].Targets[0].ID != "srv1" {
		t.Fatalf("admin2 should see srv1 inside ops, got %+v", adminGroups[0].Targets)
	}

	var bobGroups []json.RawMessage
	if err := json.Unmarshal(getJSONAs(t, router, app, "bob", "/api/groups"), &bobGroups); err != nil {
		t.Fatalf("decode bob groups: %v", err)
	}
	if len(bobGroups) != 0 {
		t.Fatalf("non-member ordinary user must see no groups, got %d", len(bobGroups))
	}
}

// TestApp_Targets_AdminSeesAllWithoutMembership mirrors the groups test for
// GET /api/targets.
func TestApp_Targets_AdminSeesAllWithoutMembership(t *testing.T) {
	app := newTestApp(t)
	seedAdminVisibilityFixtures(t, app)
	router := app.NewRouter()

	var adminTargets []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(getJSONAs(t, router, app, "admin2", "/api/targets"), &adminTargets); err != nil {
		t.Fatalf("decode admin2 targets: %v", err)
	}
	if len(adminTargets) != 1 || adminTargets[0].ID != "srv1" {
		t.Fatalf("admin2 should see srv1 without membership, got %+v", adminTargets)
	}

	var bobTargets []json.RawMessage
	if err := json.Unmarshal(getJSONAs(t, router, app, "bob", "/api/targets"), &bobTargets); err != nil {
		t.Fatalf("decode bob targets: %v", err)
	}
	if len(bobTargets) != 0 {
		t.Fatalf("non-member ordinary user must see no targets, got %d", len(bobTargets))
	}
}
