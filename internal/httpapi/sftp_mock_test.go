package httpapi

import (
	"os"
	"testing"
	"time"
)

func TestMockFileInfo_ModeAndSys(t *testing.T) {
	info := &MockFileInfo{NameField: "f", SizeField: 0, IsDirField: false, ModTimeField: time.Now()}
	if mode := info.Mode(); mode != 0 {
		t.Errorf("Mode() = %v, want 0", mode)
	}
	if sys := info.Sys(); sys != nil {
		t.Errorf("Sys() = %v, want nil", sys)
	}
	// Ensure we're covering os.FileInfo usage (e.g. in handleListFiles)
	var _ os.FileInfo = (*MockFileInfo)(nil)
}
