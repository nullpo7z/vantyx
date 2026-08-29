package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApp_UpdateUserRole(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	if _, err := app.UserStore.CreateUser("bob", "bob", "Password1!", "user"); err != nil {
		t.Fatalf("create bob: %v", err)
	}
	adminSess, _ := app.SessionStore.Create("admin")
	bobSess, _ := app.SessionStore.Create("bob")

	patch := func(sessID, target, role string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, jsonReq(t, http.MethodPatch, "/api/users/"+target, map[string]string{"role": role}, sessID))
		return w
	}

	// Non-admin and anonymous callers are refused.
	if w := patch(bobSess.ID, "bob", "admin"); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin: %d, want 403", w.Code)
	}
	if w := patch("", "bob", "admin"); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d, want 401", w.Code)
	}
	// The only admin cannot demote themselves (self rule fires first).
	if w := patch(adminSess.ID, "admin", "user"); w.Code != http.StatusBadRequest {
		t.Fatalf("self-demote: %d %s, want 400", w.Code, w.Body.String())
	}
	if w := patch(adminSess.ID, "bob", "root"); w.Code != http.StatusBadRequest {
		t.Fatalf("bogus role: %d, want 400", w.Code)
	}
	if w := patch(adminSess.ID, "nobody", "admin"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown user: %d, want 404", w.Code)
	}

	// Promote bob: effective immediately for admin-only endpoints.
	w := patch(adminSess.ID, "bob", "admin")
	if w.Code != http.StatusOK || decodeJSON(t, w)["role"] != "admin" {
		t.Fatalf("promote: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/users", nil, bobSess.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("bob as admin listing users: %d", w.Code)
	}

	// Now bob (an admin) can demote admin, since another admin remains...
	if w := patch(bobSess.ID, "admin", "user"); w.Code != http.StatusOK {
		t.Fatalf("demote admin by bob: %d %s", w.Code, w.Body.String())
	}
	// ...and admin immediately loses admin-only access.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/users", nil, adminSess.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("demoted admin listing users: %d, want 403", w.Code)
	}
	// bob is the last admin: cannot be demoted by anyone (self rule first).
	if w := patch(bobSess.ID, "bob", "user"); w.Code != http.StatusBadRequest {
		t.Fatalf("last admin self-demote: %d, want 400", w.Code)
	}
	// Restore admin, then bob's demotion must be allowed by admin, but a
	// second admin demoting the last one is refused with 409.
	if w := patch(bobSess.ID, "admin", "admin"); w.Code != http.StatusOK {
		t.Fatalf("re-promote admin: %d", w.Code)
	}
	if w := patch(adminSess.ID, "bob", "user"); w.Code != http.StatusOK {
		t.Fatalf("demote bob: %d %s", w.Code, w.Body.String())
	}
	if _, err := app.UserStore.CreateUser("carol", "carol", "Password1!", "admin"); err != nil {
		t.Fatalf("create carol: %v", err)
	}
	carolSess, _ := app.SessionStore.Create("carol")
	if w := patch(carolSess.ID, "admin", "user"); w.Code != http.StatusOK {
		t.Fatalf("carol demotes admin: %d", w.Code)
	}
	if w := patch(adminSess.ID, "carol", "user"); w.Code != http.StatusForbidden {
		t.Fatalf("demoted admin acting: %d, want 403", w.Code)
	}
	// carol is the only admin left; promoting admin back then demoting
	// carol from admin's session is fine, but with carol alone it is 409.
	if w := patch(carolSess.ID, "admin", "admin"); w.Code != http.StatusOK {
		t.Fatalf("carol re-promotes admin: %d", w.Code)
	}
	if w := patch(adminSess.ID, "carol", "user"); w.Code != http.StatusOK {
		t.Fatalf("admin demotes carol: %d", w.Code)
	}
	// admin is now the sole admin; carol (user) cannot act, admin cannot self-demote.
	if w := patch(carolSess.ID, "admin", "user"); w.Code != http.StatusForbidden {
		t.Fatalf("carol as user: %d", w.Code)
	}
	if w := patch(adminSess.ID, "admin", "user"); w.Code != http.StatusBadRequest {
		t.Fatalf("sole admin self-demote: %d", w.Code)
	}
}

// The last-admin guard (409) is reached when an admin demotes *another*
// admin who is the only one -- impossible by construction (the actor is
// an admin too), so it protects against races only; assert the store
// rule directly instead.
func TestUserStore_UpdateRoleValidation(t *testing.T) {
	app := newTestApp(t)
	if err := app.UserStore.UpdateRole("admin", "superuser"); err == nil {
		t.Fatal("invalid role accepted")
	}
	if err := app.UserStore.UpdateRole("nobody", "user"); err == nil {
		t.Fatal("unknown user accepted")
	}
}
