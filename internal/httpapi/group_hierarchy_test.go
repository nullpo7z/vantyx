package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
)

// Group access is inherited down the "parent/child" hierarchy: a member
// of "net" sees and may connect to targets in "net/tokyo", the reverse
// is not true.
func TestApp_GroupAccessInheritsToDescendants(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()

	for _, u := range []string{"parent", "child"} {
		if _, err := app.UserStore.CreateUser(u, u, "Password1!", "user"); err != nil {
			t.Fatalf("create %s: %v", u, err)
		}
	}
	for gid, tid := range map[string]string{"net": "t-net", "net/tokyo": "t-tokyo", "net/osaka": "t-osaka"} {
		if _, err := app.AccessGroupStore.Create(ctx, access.GroupID(gid), gid); err != nil {
			t.Fatalf("create group %s: %v", gid, err)
		}
		if _, err := app.TargetStore.CreateWithPath(ctx, access.TargetID(tid), tid, "127.0.0.1", 22, access.ProtocolSSH, access.GroupID(gid), gid, "", "", "", "", true, false, false); err != nil {
			t.Fatalf("create target %s: %v", tid, err)
		}
		if err := app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID(gid), access.TargetID(tid)); err != nil {
			t.Fatalf("add target %s: %v", tid, err)
		}
	}
	_ = app.AccessGroupStore.AddUserToGroup(ctx, "parent", "net")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, "child", "net/tokyo")

	groupsFor := func(user string) map[string][]string {
		sess, _ := app.SessionStore.Create(user)
		req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
		req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("/api/groups for %s: %d %s", user, w.Code, w.Body.String())
		}
		var out []struct {
			ID      string `json:"id"`
			Targets []struct {
				ID string `json:"id"`
			} `json:"targets"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		m := map[string][]string{}
		for _, g := range out {
			ids := []string{}
			for _, tg := range g.Targets {
				ids = append(ids, tg.ID)
			}
			m[g.ID] = ids
		}
		return m
	}

	parent := groupsFor("parent")
	if len(parent) != 3 || len(parent["net"]) != 1 || len(parent["net/tokyo"]) != 1 || len(parent["net/osaka"]) != 1 {
		t.Fatalf("parent sees %v, want net, net/tokyo and net/osaka each with its target", parent)
	}
	child := groupsFor("child")
	if len(child) != 1 || len(child["net/tokyo"]) != 1 {
		t.Fatalf("child sees %v, want only net/tokyo", child)
	}

	for user, tid := range map[string]string{"parent": "t-tokyo", "child": "t-tokyo"} {
		if ok, err := app.userCanAccessTarget(ctx, user, access.TargetID(tid)); err != nil || !ok {
			t.Fatalf("%s -> %s: ok=%v err=%v", user, tid, ok, err)
		}
	}
	for _, tid := range []string{"t-net", "t-osaka"} {
		if ok, _ := app.userCanAccessTarget(ctx, "child", access.TargetID(tid)); ok {
			t.Fatalf("child must not reach %s", tid)
		}
	}

	// Group invitations for net/tokyo include members inherited from net.
	members, err := app.invitableUserIDsForGroup(ctx, "child", "t-tokyo", "net/tokyo")
	if err != nil {
		t.Fatalf("invitableUserIDsForGroup: %v", err)
	}
	got := map[string]bool{}
	for _, m := range members {
		got[m] = true
	}
	if !got["parent"] || !got["child"] || len(got) != 2 {
		t.Fatalf("invitable members = %v, want parent and child", members)
	}
	if got := ancestorGroupIDs("a/b/c"); len(got) != 3 || got[0] != "a" || got[1] != "a/b" || got[2] != "a/b/c" {
		t.Fatalf("ancestorGroupIDs = %v", got)
	}
}
