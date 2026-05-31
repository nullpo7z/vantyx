package sharing

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const ddl = `CREATE TABLE session_invitations (
		id TEXT PRIMARY KEY,
		token_hash TEXT NOT NULL UNIQUE,
		session_id TEXT NOT NULL,
		session_kind TEXT NOT NULL,
		target_id TEXT NOT NULL,
		owner_user_id TEXT NOT NULL,
		invitee_user_id TEXT,
		invite_group_id TEXT,
		invite_tag TEXT,
		mode TEXT NOT NULL,
		max_uses INTEGER,
		use_count INTEGER NOT NULL DEFAULT 0,
		expires_at INTEGER NOT NULL,
		used_at INTEGER,
		revoked_at INTEGER,
		created_at INTEGER NOT NULL
	);
	CREATE TABLE session_invitation_consumers (
		invitation_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		consumed_at INTEGER NOT NULL,
		PRIMARY KEY (invitation_id, user_id)
	);`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestGenerateToken(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(tok.Plain) < 32 {
		t.Fatalf("plain token too short: %d", len(tok.Plain))
	}
	if tok.Hash == "" || tok.Hash == tok.Plain {
		t.Fatalf("hash empty or matches plain")
	}
	if tok.Hash != HashToken(tok.Plain) {
		t.Fatalf("HashToken inconsistent with GenerateToken")
	}
	tok2, _ := GenerateToken()
	if strings.EqualFold(tok.Plain, tok2.Plain) {
		t.Fatalf("two generated tokens should differ")
	}
}

func TestSQLiteStore_LifeCycle(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	tok, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	inv := Invitation{
		ID:            "inv-1",
		SessionID:     "s1",
		SessionKind:   KindTerminal,
		TargetID:      "t1",
		OwnerUserID:   "alice",
		InviteeUserID: "bob",
		Mode:          ModeViewer,
		ExpiresAt:     now.Add(15 * time.Minute),
		CreatedAt:     now,
	}
	if err := store.Create(ctx, inv, tok.Hash); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, hash, err := store.GetByID(ctx, "inv-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if hash != tok.Hash {
		t.Fatalf("hash roundtrip mismatch")
	}
	if got.ID != inv.ID || got.OwnerUserID != "alice" || got.Mode != ModeViewer {
		t.Fatalf("invitation roundtrip mismatch: %+v", got)
	}
	if got.IsLink() {
		t.Fatalf("named invitation must not be a link")
	}
	got2, err := store.GetByTokenHash(ctx, tok.Hash)
	if err != nil {
		t.Fatalf("GetByTokenHash: %v", err)
	}
	if got2.ID != inv.ID {
		t.Fatalf("token-hash lookup returned wrong row")
	}
	if err := store.RecordUse(ctx, "inv-1", "bob", now.Add(time.Minute)); err != nil {
		t.Fatalf("RecordUse: %v", err)
	}
	got3, _, _ := store.GetByID(ctx, "inv-1")
	if !got3.Used() {
		t.Fatalf("RecordUse did not stick")
	}
	if got3.InviteeUserID != "bob" {
		t.Fatalf("invitee not back-filled, got %q", got3.InviteeUserID)
	}
	if got3.Active(time.Now().UTC()) {
		t.Fatalf("used invitation must not be active")
	}
	if err := store.MarkRevoked(ctx, "inv-1", now.Add(time.Minute)); err != nil {
		t.Fatalf("MarkRevoked: %v", err)
	}
	got4, _, _ := store.GetByID(ctx, "inv-1")
	if !got4.Revoked() {
		t.Fatalf("revoke did not stick")
	}
}

func TestInvitation_Active(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name string
		inv  Invitation
		want bool
	}{
		{"fresh", Invitation{ExpiresAt: now.Add(time.Minute), CreatedAt: now}, true},
		{"expired", Invitation{ExpiresAt: now.Add(-time.Minute), CreatedAt: now}, false},
		{"revoked", Invitation{ExpiresAt: now.Add(time.Minute), RevokedAt: now}, false},
		{"used", Invitation{InviteeUserID: "bob", ExpiresAt: now.Add(time.Minute), UsedAt: now}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.inv.Active(now); got != tc.want {
				t.Fatalf("Active=%v want %v", got, tc.want)
			}
		})
	}
}

func TestSQLiteStore_ListPendingForInvitee(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mk := func(id, invitee string, used bool) {
		t.Helper()
		tok, err := GenerateToken()
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		inv := Invitation{
			ID:            id,
			SessionID:     "s1",
			SessionKind:   KindTerminal,
			TargetID:      "t1",
			OwnerUserID:   "alice",
			InviteeUserID: invitee,
			Mode:          ModeViewer,
			ExpiresAt:     now.Add(15 * time.Minute),
			CreatedAt:     now,
		}
		if err := store.Create(ctx, inv, tok.Hash); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
		if used {
			if err := store.RecordUse(ctx, id, invitee, now.Add(time.Minute)); err != nil {
				t.Fatalf("RecordUse %s: %v", id, err)
			}
		}
	}

	mk("inv-bob", "bob", false)
	mk("inv-bob-used", "bob", true)
	mk("inv-carol", "carol", false)
	mk("inv-link", "", false)

	got, err := store.ListPendingForInvitee(ctx, "bob", now)
	if err != nil {
		t.Fatalf("ListPendingForInvitee: %v", err)
	}
	if len(got) != 1 || got[0].ID != "inv-bob" {
		t.Fatalf("bob pending: got %+v", got)
	}
}

func TestSQLiteStore_LinkInvitationMultiUse(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	tok, _ := GenerateToken()
	two := 2
	inv := Invitation{
		ID:          "link-2",
		SessionID:   "s1",
		SessionKind: KindTerminal,
		TargetID:    "t1",
		OwnerUserID: "alice",
		Mode:        ModeViewer,
		MaxUses:     &two,
		ExpiresAt:   now.Add(15 * time.Minute),
		CreatedAt:   now,
	}
	if err := store.Create(ctx, inv, tok.Hash); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !inv.Active(now) {
		t.Fatalf("fresh link invite must be active")
	}
	if err := store.RecordUse(ctx, "link-2", "bob", now); err != nil {
		t.Fatalf("first RecordUse: %v", err)
	}
	got, _, _ := store.GetByID(ctx, "link-2")
	if got.UseCount != 1 || got.Used() {
		t.Fatalf("after one join: count=%d used=%v", got.UseCount, got.Used())
	}
	if !got.Active(now) {
		t.Fatalf("should still accept second join")
	}
	if err := store.RecordUse(ctx, "link-2", "carol", now.Add(time.Minute)); err != nil {
		t.Fatalf("second RecordUse: %v", err)
	}
	got2, _, _ := store.GetByID(ctx, "link-2")
	if got2.UseCount != 2 || !got2.Used() || got2.Active(now) {
		t.Fatalf("after cap: count=%d used=%v active=%v", got2.UseCount, got2.Used(), got2.Active(now))
	}
}

func TestNormaliseMode(t *testing.T) {
	if m, err := NormaliseMode(""); err != nil || m != ModeViewer {
		t.Fatalf("empty must default to viewer: %v %v", m, err)
	}
	if _, err := NormaliseMode("nope"); err == nil {
		t.Fatalf("unknown mode must fail")
	}
}
