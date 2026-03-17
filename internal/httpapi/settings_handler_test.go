package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSettings_AuditForwarder_GetAndPut_Admin(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	sess, _ := app.SessionStore.Create("admin")

	// PUT settings
	body := map[string]interface{}{
		"config": map[string]interface{}{
			"enabled": true,
			"proto":   "unixgram",
			"addr":    "",
			"app":     "vantyx-test",
			"buffer":  123,
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/audit-forwarder", bytes.NewReader(b))
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d", w.Result().StatusCode)
	}

	// GET settings should come from db
	req2 := httptest.NewRequest(http.MethodGet, "/api/settings/audit-forwarder", nil)
	req2.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Result().StatusCode != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", w2.Result().StatusCode)
	}
	var out struct {
		Config auditForwarderConfig `json:"config"`
		Source string               `json:"source"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Source != "db" {
		t.Fatalf("expected source=db, got %q", out.Source)
	}
	if !out.Config.Enabled || out.Config.Proto != "unixgram" || out.Config.App != "vantyx-test" || out.Config.Buffer != 123 {
		t.Fatalf("unexpected config: %+v", out.Config)
	}
}

func TestSettings_AuditForwarder_PutRejectsInvalidProto(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")

	b := []byte(`{"config":{"enabled":true,"proto":"bad","addr":"x:1","app":"a","buffer":1}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/audit-forwarder", bytes.NewReader(b))
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}
