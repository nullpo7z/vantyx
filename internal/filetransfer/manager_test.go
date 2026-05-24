package filetransfer

import (
	"errors"
	"testing"
)

func TestManagerCreateCancel(t *testing.T) {
	m := NewManager(t.TempDir())
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
	if snap := j.Snapshot(); snap.State != string(StateCancelled) {
		t.Fatalf("state=%s", snap.State)
	}
}

func TestManagerCancelForbidden(t *testing.T) {
	m := NewManager(t.TempDir())
	j, err := m.Create(CreateOpts{UserID: "u1", TargetID: "t1", Direction: DirectionUpload, Backend: BackendRemote}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(j.ID, "other"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("got %v", err)
	}
}

func TestJobSnapshotAndTempPath(t *testing.T) {
	m := NewManager(t.TempDir())
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
}

func TestManagerListByUser(t *testing.T) {
	m := NewManager(t.TempDir())
	_, _ = m.Create(CreateOpts{UserID: "u1", TargetID: "t1", Direction: DirectionUpload, Backend: BackendRemote}, func() {})
	_, _ = m.Create(CreateOpts{UserID: "u2", TargetID: "t2", Direction: DirectionUpload, Backend: BackendRemote}, func() {})
	list := m.ListByUser("u1")
	if len(list) != 1 {
		t.Fatalf("len=%d", len(list))
	}
}
