package access

import (
	"context"
	"testing"
	"time"
)

// Memberships with an expiry grant access only until then; expired rows
// stay listed for admins and can be purged.
func TestMembershipExpiry(t *testing.T) {
	ctx := context.Background()
	groups, _ := seedHierarchy(t) // parent is a permanent member of "net"

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	// outsider gets "net" until an hour ago: nothing is granted, including
	// the descendants that a live membership would reach.
	if err := groups.AddUserToGroupUntil(ctx, "outsider", "net", &past); err != nil {
		t.Fatalf("AddUserToGroupUntil: %v", err)
	}
	tids, _ := groups.TargetIDsForUser(ctx, "outsider", nil)
	expectSet(t, "expired member targets", targetSet(tids))
	gids, _ := groups.GroupIDsForUser(ctx, "outsider", nil)
	expectSet(t, "expired member groups", groupSet(gids))
	uids, _ := groups.UserIDsForTarget(ctx, "t-tokyo", nil)
	if userSet(uids)["outsider"] {
		t.Fatal("expired member listed as having access to t-tokyo")
	}
	members, _ := groups.UserIDsForGroup(ctx, "net", nil)
	if userSet(members)["outsider"] {
		t.Fatal("expired member returned by UserIDsForGroup")
	}
	// ...but the management listing shows the row, flagged.
	rows, err := groups.MembershipsForGroup(ctx, "net")
	if err != nil {
		t.Fatalf("MembershipsForGroup: %v", err)
	}
	seen := map[string]Membership{}
	for _, m := range rows {
		seen[string(m.UserID)] = m
	}
	if m, ok := seen["outsider"]; !ok || !m.Expired || m.ExpiresAt == nil {
		t.Fatalf("expired row = %+v, ok=%v", seen["outsider"], ok)
	}
	if m := seen["parent"]; m.Expired || m.ExpiresAt != nil {
		t.Fatalf("permanent row = %+v", m)
	}

	// Re-adding updates the expiry: now in the future -> access flows
	// down the hierarchy again.
	if err := groups.AddUserToGroupUntil(ctx, "outsider", "net", &future); err != nil {
		t.Fatal(err)
	}
	tids, _ = groups.TargetIDsForUser(ctx, "outsider", nil)
	expectSet(t, "future-expiry targets", targetSet(tids), "t-net", "t-tokyo", "t-r1", "t-osaka")
	// Plain AddUserToGroup clears the expiry (permanent).
	if err := groups.AddUserToGroup(ctx, "outsider", "net"); err != nil {
		t.Fatal(err)
	}
	rows, _ = groups.MembershipsForGroup(ctx, "net")
	for _, m := range rows {
		if m.UserID == "outsider" && m.ExpiresAt != nil {
			t.Fatal("AddUserToGroup did not clear the expiry")
		}
	}

	// Purge removes only expired rows.
	if err := groups.AddUserToGroupUntil(ctx, "outsider", "net", &past); err != nil {
		t.Fatal(err)
	}
	n, err := groups.PurgeExpiredMemberships(ctx, time.Now())
	if err != nil || n != 1 {
		t.Fatalf("purge: n=%d err=%v", n, err)
	}
	rows, _ = groups.MembershipsForGroup(ctx, "net")
	if len(rows) != 1 || rows[0].UserID != "parent" {
		t.Fatalf("after purge: %+v", rows)
	}
}
