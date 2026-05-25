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

func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "store.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	seedTestUsers(t, db, "u1", "u2")
	return NewStore(db), db
}

func sampleRecord(id, userID string, state State) JobRecord {
	now := time.Now().UTC()
	return JobRecord{
		ID:         id,
		UserID:     userID,
		TargetID:   "t1",
		TargetName: "Target 1",
		Backend:    BackendRemote,
		Direction:  DirectionDownload,
		RemotePath: "/file.bin",
		FileName:   "file.bin",
		State:      state,
		Total:      100,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestStore_InsertAndGet(t *testing.T) {
	store, _ := newTestStore(t)
	rec := sampleRecord("id-1", "u1", StateRunning)
	if err := store.Insert(context.Background(), rec); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := store.Get(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != rec.ID || got.UserID != rec.UserID || got.State != StateRunning {
		t.Fatalf("mismatch: %+v", got)
	}
	if got.Total != 100 {
		t.Fatalf("total: %d", got.Total)
	}
}

func TestStore_GetNotFound(t *testing.T) {
	store, _ := newTestStore(t)
	if _, err := store.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_UpdateState(t *testing.T) {
	store, _ := newTestStore(t)
	rec := sampleRecord("s-1", "u1", StateRunning)
	_ = store.Insert(context.Background(), rec)
	ts := time.Now().UTC().Add(time.Second)
	if err := store.UpdateState(context.Background(), "s-1", StateFailed, "boom", ts); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := store.Get(context.Background(), "s-1")
	if got.State != StateFailed || got.Error != "boom" {
		t.Fatalf("state/error: %+v", got)
	}
}

func TestStore_UpdateProgress(t *testing.T) {
	store, _ := newTestStore(t)
	rec := sampleRecord("p-1", "u1", StateRunning)
	_ = store.Insert(context.Background(), rec)
	if err := store.UpdateProgress(context.Background(), "p-1", 50, 200, time.Now()); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := store.Get(context.Background(), "p-1")
	if got.Progress != 50 || got.Total != 200 {
		t.Fatalf("progress/total: %+v", got)
	}
	// Updating with total=0 should not zero out the total.
	_ = store.UpdateProgress(context.Background(), "p-1", 75, 0, time.Now())
	got, _ = store.Get(context.Background(), "p-1")
	if got.Progress != 75 || got.Total != 200 {
		t.Fatalf("partial update: %+v", got)
	}
}

func TestStore_UpdateTempPath(t *testing.T) {
	store, _ := newTestStore(t)
	rec := sampleRecord("tp-1", "u1", StateRunning)
	_ = store.Insert(context.Background(), rec)
	if err := store.UpdateTempPath(context.Background(), "tp-1", "/tmp/abc"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := store.Get(context.Background(), "tp-1")
	if got.TempPath != "/tmp/abc" {
		t.Fatalf("temp path: %s", got.TempPath)
	}
}

func TestStore_ListByUser(t *testing.T) {
	store, _ := newTestStore(t)
	for i, id := range []string{"a", "b", "c"} {
		rec := sampleRecord(id, "u1", StateCompleted)
		rec.UpdatedAt = time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		_ = store.Insert(context.Background(), rec)
	}
	_ = store.Insert(context.Background(), sampleRecord("d", "u2", StateCompleted))

	got, err := store.ListByUser(context.Background(), "u1", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	// Newest updated_at first.
	if got[0].ID != "c" {
		t.Fatalf("expected c first, got %s", got[0].ID)
	}
}

func TestStore_ListByUserLimitDefault(t *testing.T) {
	store, _ := newTestStore(t)
	_ = store.Insert(context.Background(), sampleRecord("only", "u1", StateRunning))
	got, err := store.ListByUser(context.Background(), "u1", 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("list default: %v len=%d", err, len(got))
	}
}

func TestStore_Delete(t *testing.T) {
	store, _ := newTestStore(t)
	_ = store.Insert(context.Background(), sampleRecord("del", "u1", StateCompleted))
	rec, deleted, err := store.Delete(context.Background(), "del", "u1")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleted || rec.ID != "del" {
		t.Fatalf("delete result: deleted=%v rec=%+v", deleted, rec)
	}
	if _, err := store.Get(context.Background(), "del"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestStore_DeleteForbidden(t *testing.T) {
	store, _ := newTestStore(t)
	_ = store.Insert(context.Background(), sampleRecord("priv", "u1", StateCompleted))
	if _, _, err := store.Delete(context.Background(), "priv", "u2"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestStore_DeleteNotFound(t *testing.T) {
	store, _ := newTestStore(t)
	if _, _, err := store.Delete(context.Background(), "nope", "u1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_MarkOrphansFailed(t *testing.T) {
	store, _ := newTestStore(t)
	_ = store.Insert(context.Background(), sampleRecord("r-running", "u1", StateRunning))
	_ = store.Insert(context.Background(), sampleRecord("r-receiving", "u1", StateReceiving))
	_ = store.Insert(context.Background(), sampleRecord("r-done", "u1", StateCompleted))

	n, err := store.MarkOrphansFailed(context.Background(), time.Now(), "interrupted")
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 rows updated, got %d", n)
	}
	got, _ := store.Get(context.Background(), "r-running")
	if got.State != StateFailed || got.Error != "interrupted" {
		t.Fatalf("running not failed: %+v", got)
	}
	got, _ = store.Get(context.Background(), "r-done")
	if got.State != StateCompleted {
		t.Fatalf("terminal touched: %+v", got)
	}
}

func TestStore_TrimUserHistory(t *testing.T) {
	store, _ := newTestStore(t)
	base := time.Now().UTC()
	// 5 completed jobs for u1 with increasing updated_at.
	for i := 0; i < 5; i++ {
		rec := sampleRecord(string(rune('a'+i)), "u1", StateCompleted)
		rec.UpdatedAt = base.Add(time.Duration(i) * time.Second)
		_ = store.Insert(context.Background(), rec)
	}
	// 1 running job that must never be trimmed.
	running := sampleRecord("keep-running", "u1", StateRunning)
	_ = store.Insert(context.Background(), running)
	// Different user - must not be touched.
	_ = store.Insert(context.Background(), sampleRecord("other", "u2", StateCompleted))

	n, err := store.TrimUserHistory(context.Background(), "u1", 2)
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 rows trimmed, got %d", n)
	}
	got, _ := store.ListByUser(context.Background(), "u1", 100)
	if len(got) != 3 {
		t.Fatalf("expected 3 left (2 terminal + 1 running), got %d", len(got))
	}
	if _, err := store.Get(context.Background(), "keep-running"); err != nil {
		t.Fatalf("running was trimmed: %v", err)
	}
	if _, err := store.Get(context.Background(), "other"); err != nil {
		t.Fatalf("other user trimmed: %v", err)
	}
}

func TestStore_TrimUserHistoryZeroMaxNoop(t *testing.T) {
	store, _ := newTestStore(t)
	_ = store.Insert(context.Background(), sampleRecord("a", "u1", StateCompleted))
	n, err := store.TrimUserHistory(context.Background(), "u1", 0)
	if err != nil || n != 0 {
		t.Fatalf("trim zero: n=%d err=%v", n, err)
	}
}

func TestStore_NilStoreMethods(t *testing.T) {
	var s *Store
	if err := s.Insert(context.Background(), JobRecord{}); err == nil {
		t.Fatal("expected error on nil store insert")
	}
	if err := s.UpdateState(context.Background(), "x", StateFailed, "", time.Now()); err == nil {
		t.Fatal("expected error on nil store update state")
	}
	if err := s.UpdateProgress(context.Background(), "x", 1, 1, time.Now()); err == nil {
		t.Fatal("expected error on nil store update progress")
	}
	if err := s.UpdateTempPath(context.Background(), "x", "t"); err == nil {
		t.Fatal("expected error on nil store update temp")
	}
	if _, err := s.Get(context.Background(), "x"); err == nil {
		t.Fatal("expected error on nil store get")
	}
	if _, err := s.ListByUser(context.Background(), "u", 1); err == nil {
		t.Fatal("expected error on nil store list")
	}
	if _, _, err := s.Delete(context.Background(), "x", "u"); err == nil {
		t.Fatal("expected error on nil store delete")
	}
	if _, err := s.MarkOrphansFailed(context.Background(), time.Now(), ""); err == nil {
		t.Fatal("expected error on nil store reap")
	}
	if _, err := s.TrimUserHistory(context.Background(), "u", 1); err == nil {
		t.Fatal("expected error on nil store trim")
	}
	if got := NewStore(nil); got != nil {
		t.Fatal("NewStore(nil) should return nil")
	}
}
