package httpapi

import (
	"context"
	"testing"
)

func TestNewCommandLogStore_NilDB(t *testing.T) {
	if got := newCommandLogStore(nil); got != nil {
		t.Fatalf("expected nil store")
	}
}

func TestCommandLogRecorder_RecordInputLines(t *testing.T) {
	app := newTestApp(t)
	store := newCommandLogStore(app.DB)
	rec := newCommandLogRecorder(store, "sess1", "admin", "t1")
	if rec == nil {
		t.Fatal("expected recorder")
	}

	rec.RecordInput([]byte("ls -la"))
	rec.RecordInput([]byte("\n"))
	rec.RecordInput([]byte("echo hi\r\n"))
	rec.RecordInput([]byte("\n"))
	rec.RecordInput(nil)
	rec.RecordInput([]byte("   \n"))

	var count int
	err := app.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM command_logs WHERE session_id = ?`, "sess1",
	).Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 lines recorded, got %d", count)
	}
}

func TestCommandLogStore_AppendLineSkipsEmpty(t *testing.T) {
	app := newTestApp(t)
	store := newCommandLogStore(app.DB)
	store.appendLine(context.Background(), "s", "u", "t", "   ")
	store.appendLine(context.Background(), "s", "u", "t", "ok")

	var count int
	_ = app.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM command_logs WHERE line_text = 'ok'`,
	).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 non-empty line, got %d", count)
	}
}

func TestNewCommandLogRecorder_NilStore(t *testing.T) {
	if got := newCommandLogRecorder(nil, "s", "u", "t"); got != nil {
		t.Fatal("expected nil recorder")
	}
}
