package httpapi

import (
	"bytes"
	"context"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
)

func TestTargets_ImportExportCheck(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	for _, g := range []string{"net", "net/tokyo"} {
		if _, err := app.AccessGroupStore.Create(ctx, access.GroupID(g), g); err != nil {
			t.Fatal(err)
		}
	}
	_ = app.AccessGroupStore.SetGroupTags(ctx, "net/tokyo", []string{"tokyo"})
	if _, err := app.UserStore.CreateUser("bob", "bob", "Password1!", "user"); err != nil {
		t.Fatal(err)
	}
	adminSess, _ := app.SessionStore.Create("admin")
	bobSess, _ := app.SessionStore.Create("bob")

	// A listening port for the reachability check.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	openPort := ln.Addr().(*net.TCPAddr).Port
	closedLn, _ := net.Listen("tcp", "127.0.0.1:0")
	closedPort := closedLn.Addr().(*net.TCPAddr).Port
	_ = closedLn.Close()

	upload := func(sessID, filename, content string, dryRun bool) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", filename)
		_, _ = fw.Write([]byte(content))
		if dryRun {
			_ = mw.WriteField("dry_run", "1")
		}
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/targets/import", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		if sessID != "" {
			req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	csvBody := "name,host,port,protocol,group_id,tags,ssh_username,sftp_enabled\n" +
		"router1,127.0.0.1," + strconv.Itoa(openPort) + ",ssh,net/tokyo,core edge,admin,true\n" +
		"router2,127.0.0.1," + strconv.Itoa(closedPort) + ",telnet,net,,,\n" +
		"bad,10.0.0.9,22,ssh,missing-group,,,\n" +
		"noport,10.0.0.5,,ssh,net,,,\n"

	if w := upload(bobSess.ID, "t.csv", csvBody, true); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin: %d", w.Code)
	}
	// Dry run creates nothing but reports validity per row.
	w := upload(adminSess.ID, "t.csv", csvBody, true)
	if w.Code != http.StatusOK {
		t.Fatalf("dry run: %d %s", w.Code, w.Body.String())
	}
	dry := decodeJSON(t, w)
	if dry["dry_run"] != true || dry["failed"] != float64(1) || dry["created"] != float64(0) {
		t.Fatalf("dry = %v", dry)
	}
	if ids, _ := app.TargetStore.AllIDs(ctx, nil); len(ids) != 0 {
		t.Fatalf("dry run created targets: %v", ids)
	}
	// Real import: 3 created, 1 error; a second import skips the duplicates.
	w = upload(adminSess.ID, "t.csv", csvBody, false)
	res := decodeJSON(t, w)
	if w.Code != http.StatusOK || res["created"] != float64(3) || res["failed"] != float64(1) {
		t.Fatalf("import: %d %v", w.Code, res)
	}
	r1, err := app.TargetStore.Get(ctx, "router1")
	if err != nil || r1.Port != uint16(openPort) || r1.SSHUsername != "admin" || !r1.SFTPEnabled {
		t.Fatalf("router1 = %+v err=%v", r1, err)
	}
	tags, _ := app.TargetStore.TagsForTarget(ctx, "router1")
	if strings.Join(tags, ",") != "core,edge,tokyo" && strings.Join(tags, ",") != "core,tokyo,edge" {
		sortTags := append([]string(nil), tags...)
		sortStrings(sortTags)
		if strings.Join(sortTags, ",") != "core,edge,tokyo" {
			t.Fatalf("tags = %v (own + inherited group tag expected)", tags)
		}
	}
	if np, _ := app.TargetStore.Get(ctx, "noport"); np == nil || np.Port != 22 {
		t.Fatalf("default port not applied: %+v", np)
	}
	gids, _ := app.AccessGroupStore.GroupIDsForTarget(ctx, "router1")
	if len(gids) != 1 || gids[0] != "net/tokyo" {
		t.Fatalf("group assignment = %v", gids)
	}
	w = upload(adminSess.ID, "t.csv", csvBody, false)
	res = decodeJSON(t, w)
	if res["created"] != float64(0) || res["skipped"] != float64(3) {
		t.Fatalf("re-import = %v", res)
	}
	// JSON import path.
	w = upload(adminSess.ID, "t.json", `[{"name":"vnc1","host":"10.0.0.7","protocol":"vnc","group_id":"net"}]`, false)
	if w.Code != http.StatusOK || decodeJSON(t, w)["created"] != float64(1) {
		t.Fatalf("json import: %d %s", w.Code, w.Body.String())
	}
	if v, _ := app.TargetStore.Get(ctx, "vnc1"); v == nil || v.Port != 5900 {
		t.Fatalf("vnc default port: %+v", v)
	}
	if w := upload(adminSess.ID, "bad.csv", "host\n1.2.3.4\n", false); w.Code != http.StatusBadRequest {
		t.Fatalf("csv without name header: %d", w.Code)
	}

	// Export CSV (no password column content) and JSON.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/targets/export", nil, adminSess.ID))
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("export csv: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	body := w.Body.String()
	if !strings.HasPrefix(body, strings.Join(targetCSVHeader, ",")) || !strings.Contains(body, "router1,127.0.0.1,"+strconv.Itoa(openPort)+",ssh,net/tokyo,") {
		t.Fatalf("export body:\n%s", body)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/targets/export?format=json", nil, adminSess.ID))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"router1"`) || strings.Contains(w.Body.String(), "ssh_password") {
		t.Fatalf("export json: %d %s", w.Code, w.Body.String())
	}

	// Reachability: open port reachable, closed port not.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/targets/check", map[string]interface{}{"ids": []string{"router1", "router2"}}, adminSess.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("check: %d %s", w.Code, w.Body.String())
	}
	chk := w.Body.String()
	if !strings.Contains(chk, `"id":"router1","host":"127.0.0.1","port":`+strconv.Itoa(openPort)+`,"reachable":true`) {
		t.Fatalf("router1 not reachable: %s", chk)
	}
	if !strings.Contains(chk, `"id":"router2"`) || !strings.Contains(chk, `"reachable":false`) {
		t.Fatalf("router2 should be unreachable: %s", chk)
	}
}

func TestTargetsCSV_FormulaGuardRoundTrips(t *testing.T) {
	for _, in := range []string{"=1+1", "+cmd", "-x", "@SUM", "\tx", "plain", "", "'quoted"} {
		safe := csvSafe(in)
		if in != "" && strings.ContainsAny(in[:1], "=+-@\t\r") && !strings.HasPrefix(safe, "'") {
			t.Errorf("csvSafe(%q) = %q, not neutralised", in, safe)
		}
		if got := csvUnsafe(safe); got != in {
			t.Errorf("round trip %q -> %q -> %q", in, safe, got)
		}
	}
	rows, err := parseTargetCSV([]byte("name,host,group_id,path\n'=evil,10.0.0.1,g1,g1/rack\n"))
	if err != nil || len(rows) != 1 || rows[0].Name != "=evil" || rows[0].Path != "g1/rack" {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
}
