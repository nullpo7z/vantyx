package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
)

func deleteUserAs(t *testing.T, app *App, actor, target string) *httptest.ResponseRecorder {
	t.Helper()
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create(actor)
	req := httptest.NewRequest(http.MethodDelete, "/api/users/"+target, nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestApp_DeleteUser_RemovesUserAndMemberships is the E-11 regression
// guard: DELETE /api/users/{id} (previously 405) removes the account and
// its group membership / tags via cascade, revokes invitations addressed
// to it, and leaves a user_delete audit entry.
func TestApp_DeleteUser_RemovesUserAndMemberships(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	if _, err := app.UserStore.CreateUser("bob", "bob", "BobPass1!", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("bob"), access.GroupID("g1"))
	_ = app.UserStore.SetUserTags("bob", []string{"ops"})

	w := deleteUserAs(t, app, "admin", "bob")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", w.Code, w.Body.String())
	}
	if _, err := app.UserStore.GetByID("bob"); err == nil {
		t.Fatal("expected bob to be gone after delete")
	}
	members, _ := app.AccessGroupStore.UserIDsForGroup(ctx, access.GroupID("g1"), nil)
	for _, m := range members {
		if m == access.UserID("bob") {
			t.Fatal("expected bob's group membership to be removed (cascade)")
		}
	}
	e, ok := latestAuditEvent("user_delete")
	if !ok {
		t.Fatal("expected a user_delete audit entry")
	}
	if e.Fields["deleted_user_id"] != "bob" || e.Fields["user_id"] != "admin" {
		t.Fatalf("unexpected user_delete fields: %v", e.Fields)
	}
}

// TestApp_DeleteUser_GuardRails covers the refusal paths: self-deletion,
// unknown user, and a non-admin actor. It also checks that with two admins
// one may be deleted and exactly one remains (the last-admin guard is a
// defense-in-depth check: through HTTP the actor is always an admin
// distinct from the target, so a sole admin can only ever be the actor
// themselves, which the self-deletion rule already refuses).
func TestApp_DeleteUser_GuardRails(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.UserStore.CreateUser("bob", "bob", "BobPass1!", ""); err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}
	if _, err := app.UserStore.CreateUser("admin2", "admin2", "Admin2Pass1!", "admin"); err != nil {
		t.Fatalf("CreateUser admin2: %v", err)
	}

	if w := deleteUserAs(t, app, "admin", "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("self delete: expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if w := deleteUserAs(t, app, "admin", "nobody"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown user: expected 404, got %d", w.Code)
	}
	if w := deleteUserAs(t, app, "bob", "admin2"); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin actor: expected 403, got %d", w.Code)
	}
	if _, err := app.UserStore.GetByID("admin2"); err != nil {
		t.Fatalf("admin2 must survive a forbidden delete: %v", err)
	}

	// Two admins: deleting one is allowed and leaves exactly one.
	if w := deleteUserAs(t, app, "admin", "admin2"); w.Code != http.StatusNoContent {
		t.Fatalf("delete second admin: expected 204, got %d body=%s", w.Code, w.Body.String())
	}
	if n, err := app.countAdmins(); err != nil || n != 1 {
		t.Fatalf("countAdmins = %d, %v; want 1", n, err)
	}
	// The survivor can only be targeted by itself now -> refused.
	if w := deleteUserAs(t, app, "admin", "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("sole admin self delete: expected 400, got %d", w.Code)
	}
}
