package httpapi

import "testing"

func TestSettingsStore_SaveAndLoadAuditForwarderConfig(t *testing.T) {
	app := newTestApp(t)

	want := auditForwarderConfig{Enabled: true, Proto: "udp", Addr: "127.0.0.1:514", App: "vantyx", Buffer: 12}
	if err := saveAuditForwarderConfigToDB(app.DB, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok := loadAuditForwarderConfigFromDB(app.DB)
	if !ok {
		t.Fatal("expected ok")
	}
	if got != want {
		t.Fatalf("unexpected config: got=%+v want=%+v", got, want)
	}
}

