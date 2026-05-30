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

func TestCommandLogRecorder_TabCompletionEcho(t *testing.T) {
	app := newTestApp(t)
	store := newCommandLogStore(app.DB)
	rec := newCommandLogRecorder(store, "sess-tab", "admin", "t1")

	rec.RecordInput([]byte("ls /u"))
	rec.recordStdout([]byte("\r\x1b"))
	rec.recordStdout([]byte("[Kls /usr/bin/"))
	rec.RecordInput([]byte("\n"))

	var line string
	err := app.DB.QueryRowContext(context.Background(),
		`SELECT line_text FROM command_logs WHERE session_id = ?`, "sess-tab",
	).Scan(&line)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if line != "ls /usr/bin/" {
		t.Fatalf("expected completed line, got %q", line)
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
	rec.recordStdout([]byte("ls -la"))
	rec.RecordInput([]byte("\n"))
	rec.RecordInput([]byte("echo hi\r\n"))
	rec.recordStdout([]byte("echo hi"))
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

func TestCommandLogStdoutWriter_Write(t *testing.T) {
	app := newTestApp(t)
	store := newCommandLogStore(app.DB)
	rec := newCommandLogRecorder(store, "sess-w", "admin", "t1")
	w := commandLogStdoutWriter{rec: rec}
	payload := []byte("echo test")
	n, err := w.Write(payload)
	if err != nil || n != len(payload) {
		t.Fatalf("Write: n=%d err=%v", n, err)
	}
	if rec.echo.currentLine() != "echo test" {
		t.Fatalf("echo line: %q", rec.echo.currentLine())
	}
	rec.RecordInput([]byte("\n"))
	var line string
	_ = app.DB.QueryRowContext(context.Background(),
		`SELECT line_text FROM command_logs WHERE session_id = ?`, "sess-w",
	).Scan(&line)
	if line != "echo test" {
		t.Fatalf("got %q", line)
	}
}

func TestCommandLogRecorder_SkipsPasswordWithoutEcho(t *testing.T) {
	app := newTestApp(t)
	store := newCommandLogStore(app.DB)
	rec := newCommandLogRecorder(store, "sess-pw", "admin", "t1")

	rec.RecordInput([]byte("S3cret!"))
	rec.RecordInput([]byte("\n"))

	var count int
	err := app.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM command_logs WHERE session_id = ?`, "sess-pw",
	).Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected password input to be skipped, got %d rows", count)
	}
}

func TestCommandLogRecorder_SkipsPromptRedraw(t *testing.T) {
	app := newTestApp(t)
	store := newCommandLogStore(app.DB)
	rec := newCommandLogRecorder(store, "sess-prompt", "admin", "t1")

	rec.recordStdout([]byte("\x1b]0;nullpo7z@claude: ~\x07nullpo7z@claude:~$ "))
	rec.RecordInput([]byte("\n"))

	var count int
	err := app.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM command_logs WHERE session_id = ?`, "sess-prompt",
	).Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected prompt redraw to be skipped, got %d rows", count)
	}
}

func TestNewCommandLogRecorder_NilStore(t *testing.T) {
	if got := newCommandLogRecorder(nil, "s", "u", "t"); got != nil {
		t.Fatal("expected nil recorder")
	}
}
