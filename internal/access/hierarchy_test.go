package access

import (
	"context"
	"testing"
)

// seedHierarchy builds
//
//	net            (t-net)      tag: net
//	net/tokyo      (t-tokyo)    tag: net_tokyo
//	net/tokyo/r1   (t-r1)
//	net/osaka      (t-osaka)
//	net_x          (t-netx)     -- "_" sibling that must NOT be treated as a child of "net"
//	other          (t-other)
//
// with users: parent (member of net), child (member of net/tokyo),
// tagged (user tag "net"), and outsider (nothing).
func seedHierarchy(t *testing.T) (*SQLiteAccessGroupStore, *SQLiteTargetStore) {
	t.Helper()
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	db := groups.db
	for _, u := range []string{"parent", "child", "tagged", "outsider"} {
		if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO users (id, username, password_hash) VALUES (?, ?, 'hash')`, u, u); err != nil {
			t.Fatalf("insert user %s: %v", u, err)
		}
	}
	for gid, tid := range map[string]string{
		"net": "t-net", "net/tokyo": "t-tokyo", "net/tokyo/r1": "t-r1", "net/osaka": "t-osaka", "net_x": "t-netx", "other": "t-other",
	} {
		if _, err := groups.Create(ctx, GroupID(gid), gid); err != nil {
			t.Fatalf("create group %s: %v", gid, err)
		}
		if _, err := targets.CreateWithPath(ctx, TargetID(tid), tid, "h", 22, ProtocolSSH, GroupID(gid), gid, "", "", "", "", true, false, false); err != nil {
			t.Fatalf("create target %s: %v", tid, err)
		}
		if err := groups.AddTargetToGroup(ctx, GroupID(gid), TargetID(tid)); err != nil {
			t.Fatalf("add target %s: %v", tid, err)
		}
	}
	_ = groups.SetGroupTags(ctx, "net", []string{"net"})
	_ = groups.SetGroupTags(ctx, "net/tokyo", []string{"net_tokyo"})
	if err := groups.AddUserToGroup(ctx, "parent", "net"); err != nil {
		t.Fatalf("add parent: %v", err)
	}
	if err := groups.AddUserToGroup(ctx, "child", "net/tokyo"); err != nil {
		t.Fatalf("add child: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO user_tags (user_id, tag) VALUES ('tagged', 'net')`); err != nil {
		t.Fatalf("tag user: %v", err)
	}
	return groups, targets
}

func targetSet(ids []TargetID) map[string]bool {
	m := map[string]bool{}
	for _, id := range ids {
		m[string(id)] = true
	}
	return m
}

func groupSet(ids []GroupID) map[string]bool {
	m := map[string]bool{}
	for _, id := range ids {
		m[string(id)] = true
	}
	return m
}

func userSet(ids []UserID) map[string]bool {
	m := map[string]bool{}
	for _, id := range ids {
		m[string(id)] = true
	}
	return m
}

func expectSet(t *testing.T, what string, got map[string]bool, want ...string) {
	t.Helper()
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w] = true
	}
	for k := range got {
		if !wantSet[k] {
			t.Errorf("%s: unexpected %q in %v", what, k, got)
		}
	}
	for k := range wantSet {
		if !got[k] {
			t.Errorf("%s: missing %q in %v", what, k, got)
		}
	}
}

func TestHierarchy_ParentMembershipReachesDescendants(t *testing.T) {
	ctx := context.Background()
	groups, _ := seedHierarchy(t)

	tids, err := groups.TargetIDsForUser(ctx, "parent", nil)
	if err != nil {
		t.Fatalf("TargetIDsForUser: %v", err)
	}
	expectSet(t, "parent targets", targetSet(tids), "t-net", "t-tokyo", "t-r1", "t-osaka")

	gids, err := groups.GroupIDsForUser(ctx, "parent", nil)
	if err != nil {
		t.Fatalf("GroupIDsForUser: %v", err)
	}
	expectSet(t, "parent groups", groupSet(gids), "net", "net/tokyo", "net/tokyo/r1", "net/osaka")
}

func TestHierarchy_ChildMembershipDoesNotClimb(t *testing.T) {
	ctx := context.Background()
	groups, _ := seedHierarchy(t)

	tids, _ := groups.TargetIDsForUser(ctx, "child", nil)
	expectSet(t, "child targets", targetSet(tids), "t-tokyo", "t-r1")
	gids, _ := groups.GroupIDsForUser(ctx, "child", nil)
	expectSet(t, "child groups", groupSet(gids), "net/tokyo", "net/tokyo/r1")

	tids, _ = groups.TargetIDsForUser(ctx, "outsider", nil)
	expectSet(t, "outsider targets", targetSet(tids))
}

func TestHierarchy_GroupTagInherits(t *testing.T) {
	ctx := context.Background()
	groups, _ := seedHierarchy(t)

	tids, _ := groups.TargetIDsForUser(ctx, "tagged", nil)
	expectSet(t, "tagged targets", targetSet(tids), "t-net", "t-tokyo", "t-r1", "t-osaka")
	gids, _ := groups.GroupIDsForUser(ctx, "tagged", nil)
	expectSet(t, "tagged groups", groupSet(gids), "net", "net/tokyo", "net/tokyo/r1", "net/osaka")
}

func TestHierarchy_UnderscoreSiblingIsNotAChild(t *testing.T) {
	ctx := context.Background()
	groups, _ := seedHierarchy(t)
	// "net_x" shares the prefix "net" but is not "net/..."; a LIKE-based
	// prefix test would also let "a_b" match "a/b".
	for _, u := range []string{"parent", "tagged"} {
		tids, _ := groups.TargetIDsForUser(ctx, UserID(u), nil)
		if targetSet(tids)["t-netx"] {
			t.Errorf("%s reaches t-netx through the underscore sibling", u)
		}
	}
	if _, err := groups.Create(ctx, "a_b", "a_b"); err != nil {
		t.Fatal(err)
	}
	if _, err := groups.Create(ctx, "a", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := groups.Create(ctx, "a/b", "a/b"); err != nil {
		t.Fatal(err)
	}
	_, _ = groups.db.ExecContext(ctx, `INSERT OR IGNORE INTO users (id, username, password_hash) VALUES ('ab', 'ab', 'hash')`)
	_ = groups.AddUserToGroup(ctx, "ab", "a_b")
	gids, _ := groups.GroupIDsForUser(ctx, "ab", nil)
	expectSet(t, "a_b member groups", groupSet(gids), "a_b")
}

func TestHierarchy_ReverseLookupsIncludeAncestors(t *testing.T) {
	ctx := context.Background()
	groups, _ := seedHierarchy(t)

	uids, err := groups.UserIDsForTarget(ctx, "t-r1", nil)
	if err != nil {
		t.Fatalf("UserIDsForTarget: %v", err)
	}
	expectSet(t, "users for t-r1", userSet(uids), "parent", "child", "tagged")

	uids, _ = groups.UserIDsForTarget(ctx, "t-osaka", nil)
	expectSet(t, "users for t-osaka", userSet(uids), "parent", "tagged")

	tags, err := groups.TagsGrantingTargetAccess(ctx, "t-r1")
	if err != nil {
		t.Fatalf("TagsGrantingTargetAccess: %v", err)
	}
	m := map[string]bool{}
	for _, tg := range tags {
		m[tg] = true
	}
	expectSet(t, "tags for t-r1", m, "net", "net_tokyo")

	// Direct membership listing stays direct: the management UI edits it.
	members, _ := groups.UserIDsForGroup(ctx, "net/tokyo", nil)
	expectSet(t, "direct members of net/tokyo", userSet(members), "child")
}
