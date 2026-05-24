package filetransfer

import "testing"

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

func TestManagerListByUser(t *testing.T) {
	m := NewManager(t.TempDir())
	_, _ = m.Create(CreateOpts{UserID: "u1", TargetID: "t1", Direction: DirectionUpload, Backend: BackendRemote}, func() {})
	_, _ = m.Create(CreateOpts{UserID: "u2", TargetID: "t2", Direction: DirectionUpload, Backend: BackendRemote}, func() {})
	list := m.ListByUser("u1")
	if len(list) != 1 {
		t.Fatalf("len=%d", len(list))
	}
}
