package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
)

// newTestManager creates a Manager backed by a temporary on-disk SQLite database.
// The returned Manager has a valid Store; the database is closed via t.Cleanup.
// A few placeholder users are inserted so foreign keys on file_transfer_jobs pass.
func newTestManager(t *testing.T) (*Manager, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "filetransfer.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: path})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	seedTestUsers(t, db, "u1", "u2", "other")
	store := NewStore(db)
	return NewManager(t.TempDir(), store), db
}

func seedTestUsers(t *testing.T, db *sql.DB, ids ...string) {
	t.Helper()
	for _, id := range ids {
		_, err := db.Exec(
			`INSERT OR IGNORE INTO users (id, username, password_hash) VALUES (?, ?, '')`,
			id, id,
		)
		if err != nil {
			t.Fatalf("seed user %s: %v", id, err)
		}
	}
}

func TestManagerCreateCancel(t *testing.T) {
	m, _ := newTestManager(t)
	cancel := func() {}
	j, err := m.Create(CreateOpts{
		UserID:     "u1",
		TargetID:   "t1",
		Direction:  DirectionDownload,
		Backend:    BackendRemote,
		FileName:   "a.txt",
		RemotePath: "/a.txt",
	}, cancel)
	if err != nil {
		t.Fatal(err)
	}
	if j.ID == "" {
		t.Fatal("expected id")
	}
	got, ok := m.Get(j.ID)
	if !ok || got.UserID != "u1" {
		t.Fatal("get failed")
	}
	if err := m.Cancel(j.ID, "u1"); err != nil {
		t.Fatal(err)
	}
	got, _ = m.Get(j.ID)
	if snap := got.Snapshot(); snap.State != string(StateCancelled) {
		t.Fatalf("state=%s", snap.State)
	}
}

func TestManagerCancelForbidden(t *testing.T) {
	m, _ := newTestManager(t)
	j, err := m.Create(CreateOpts{UserID: "u1", TargetID: "t1", Direction: DirectionUpload, Backend: BackendRemote}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(j.ID, "other"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("got %v", err)
	}
}

func TestJobSnapshotAndTempPath(t *testing.T) {
	m, _ := newTestManager(t)
	j, err := m.Create(CreateOpts{
		UserID: "u1", TargetID: "t1", Direction: DirectionDownload,
		Backend: BackendRemote, FileName: "a.bin", RemotePath: "/a.bin",
	}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	j.SetTempPath("/tmp/x")
	j.SetProgress(10, 100)
	snap := j.Snapshot()
	if snap.Progress != 10 || snap.Total != 100 || snap.FileName != "a.bin" {
		t.Fatalf("snap=%+v", snap)
	}
	if j.GetTempPath() != "/tmp/x" || j.GetFileName() != "a.bin" {
		t.Fatal("getters mismatch")
	}
	got, ok := m.Get(j.ID)
	if !ok {
		t.Fatal("lost job")
	}
	if got.TempPath != "/tmp/x" {
		t.Fatalf("temp path not persisted: %q", got.TempPath)
	}
}

func TestManagerListByUser(t *testing.T) {
	m, _ := newTestManager(t)
	_, _ = m.Create(CreateOpts{UserID: "u1", TargetID: "t1", Direction: DirectionUpload, Backend: BackendRemote}, func() {})
	_, _ = m.Create(CreateOpts{UserID: "u2", TargetID: "t2", Direction: DirectionUpload, Backend: BackendRemote}, func() {})
	list := m.ListByUser("u1")
	if len(list) != 1 {
		t.Fatalf("len=%d", len(list))
	}
}

func TestManagerPersistsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ft.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	defer db.Close()

	seedTestUsers(t, db, "u1")
	m1 := NewManager(t.TempDir(), NewStore(db))
	j, err := m1.Create(CreateOpts{
		UserID: "u1", TargetID: "t1", Direction: DirectionDownload,
		Backend: BackendRemote, FileName: "x.bin", RemotePath: "/x.bin",
		InitialState: StateRunning,
	}, func() {})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	m2 := NewManager(t.TempDir(), NewStore(db))
	if _, err := m2.ReapOrphans(context.Background(), "restart"); err != nil {
		t.Fatalf("reap: %v", err)
	}
	got, ok := m2.Get(j.ID)
	if !ok {
		t.Fatal("job missing after restart")
	}
	if got.State != StateFailed {
		t.Fatalf("expected failed after reap, got %s", got.State)
	}
	if got.Error != "restart" {
		t.Fatalf("expected error message, got %q", got.Error)
	}
}

func TestManagerCancelInvokesCancelFunc(t *testing.T) {
	m, _ := newTestManager(t)
	called := false
	j, err := m.Create(CreateOpts{
		UserID: "u1", TargetID: "t1", Direction: DirectionDownload, Backend: BackendRemote,
	}, func() { called = true })
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(j.ID, "u1"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("cancel func was not called")
	}
}

func TestManagerCancelAlreadyTerminal(t *testing.T) {
	m, _ := newTestManager(t)
	j, _ := m.Create(CreateOpts{UserID: "u1", TargetID: "t1", Direction: DirectionUpload, Backend: BackendRemote}, func() {})
	j.SetState(StateCompleted, "")
	if err := m.Cancel(j.ID, "u1"); err != nil {
		t.Fatalf("expected nil on terminal cancel, got %v", err)
	}
	got, _ := m.Get(j.ID)
	if got.State != StateCompleted {
		t.Fatalf("state changed unexpectedly: %s", got.State)
	}
}

func TestManagerTrimHistoryEnforcesMax(t *testing.T) {
	m, _ := newTestManager(t)
	m.SetMaxHistoryPerUser(3)
	for i := 0; i < 5; i++ {
		j, _ := m.Create(CreateOpts{UserID: "u1", TargetID: "t1", Direction: DirectionUpload, Backend: BackendRemote}, func() {})
		j.SetState(StateCompleted, "")
		time.Sleep(time.Millisecond)
	}
	list := m.ListByUser("u1")
	if len(list) != 3 {
		t.Fatalf("expected 3 jobs after trim, got %d", len(list))
	}
}

func TestManagerRemove(t *testing.T) {
	m, _ := newTestManager(t)
	j, _ := m.Create(CreateOpts{UserID: "u1", TargetID: "t1", Direction: DirectionDownload, Backend: BackendRemote}, func() {})
	m.Remove(j.ID)
	if _, ok := m.Get(j.ID); ok {
		t.Fatal("job still present after Remove")
	}
}

func TestManagerNotifierFires(t *testing.T) {
	m, _ := newTestManager(t)
	events := 0
	var lastUser string
	m.SetNotifier(func(_ JobSnapshot, userID string) {
		events++
		lastUser = userID
	})
	j, _ := m.Create(CreateOpts{UserID: "u1", TargetID: "t1", Direction: DirectionDownload, Backend: BackendRemote}, func() {})
	j.SetState(StateCompleted, "")
	if events < 2 {
		t.Fatalf("expected >=2 notifier events, got %d", events)
	}
	if lastUser != "u1" {
		t.Fatalf("wrong userID: %s", lastUser)
	}
}

func TestManagerNilStore(t *testing.T) {
	m := NewManager(t.TempDir(), nil)
	if _, ok := m.Get("any"); ok {
		t.Fatal("expected miss with nil store")
	}
	if got := m.ListByUser("u1"); got != nil {
		t.Fatalf("expected nil list, got %v", got)
	}
	if err := m.Cancel("any", "u1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if _, err := m.ReapOrphans(context.Background(), "x"); err != nil {
		t.Fatalf("reap on nil store: %v", err)
	}
}
