package recording

import "testing"

func TestNewGovernor_DefaultLimit(t *testing.T) {
	g := NewGovernor()
	if g.LimitFraction() != defaultResourceLimit {
		t.Fatalf("limit %v", g.LimitFraction())
	}
	if g.MaxConcurrent() < 1 {
		t.Fatal("expected at least one live slot")
	}
	if g.MaxExportConcurrent() < 1 {
		t.Fatal("expected at least one export slot")
	}
	if g.FFmpegThreads() < 1 {
		t.Fatal("expected at least one ffmpeg thread")
	}
}

func TestGovernor_AcquireRelease(t *testing.T) {
	g := &Governor{
		limit:               0.8,
		maxLiveConcurrent:   1,
		maxExportConcurrent: 1,
		ffmpegThreads:       1,
		liveSem:             make(chan struct{}, 1),
		exportSem:           make(chan struct{}, 1),
	}
	ctx := t.Context()
	if err := g.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	g.Release()
	if err := g.AcquireExport(ctx); err != nil {
		t.Fatal(err)
	}
	g.ReleaseExport()
}
