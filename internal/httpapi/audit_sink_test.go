package httpapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuditSink_FileMode0600(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "audit.log")
	s, err := newAuditSink(nil, p, nil)
	if err != nil {
		t.Fatalf("newAuditSink: %v", err)
	}
	if s.file == nil {
		t.Fatal("expected file to be opened")
	}
	_ = s.file.Close()

	st, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Mode().Perm() != 0600 {
		t.Fatalf("expected 0600, got %o", st.Mode().Perm())
	}
}
