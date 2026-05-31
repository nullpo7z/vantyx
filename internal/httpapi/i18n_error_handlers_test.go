package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
)

type localizedHandlerCase struct {
	name     string
	method   string
	path     string
	body     string
	wantCode int
	wantSub  string
}

func japaneseLocalizedHandlerFixture(t *testing.T) (*App, http.Handler, string) {
	t.Helper()
	app := newTestApp(t)
	router := app.NewRouter()
	if err := app.UserStore.UpdateLocale("admin", "ja"); err != nil {
		t.Fatalf("UpdateLocale: %v", err)
	}
	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("SessionStore.Create: %v", err)
	}
	ctx := context.Background()
	if _, err := app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1"); err != nil {
		t.Fatalf("Create g1: %v", err)
	}
	if err := app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1")); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	return app, router, sess.ID
}

func runLocalizedHandlerCases(t *testing.T, sessionID string, router http.Handler, cases []localizedHandlerCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.body != "" {
				body = bytes.NewReader([]byte(tc.body))
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessionID, Path: "/"})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Result().StatusCode != tc.wantCode {
				t.Fatalf("status: got %d, want %d (body=%s)", w.Result().StatusCode, tc.wantCode, w.Body.String())
			}
			if msg := decodeErrorMessage(t, w.Body.Bytes()); !strings.Contains(msg, tc.wantSub) {
				t.Fatalf("expected localized message containing %q, got %q", tc.wantSub, msg)
			}
		})
	}
}

func TestApp_ErrorMessage_LocalizedHandlers_Groups(t *testing.T) {
	_, router, sessID := japaneseLocalizedHandlerFixture(t)
	runLocalizedHandlerCases(t, sessID, router, []localizedHandlerCase{
		{
			name:     "name is required (ja)",
			method:   http.MethodPost,
			path:     "/api/groups",
			body:     `{"name":"  "}`,
			wantCode: http.StatusBadRequest,
			wantSub:  "必須",
		},
	})
}

func TestApp_ErrorMessage_LocalizedHandlers_Recordings(t *testing.T) {
	_, router, sessID := japaneseLocalizedHandlerFixture(t)
	runLocalizedHandlerCases(t, sessID, router, []localizedHandlerCase{
		{
			name:     "bad format",
			method:   http.MethodGet,
			path:     "/api/recordings/abc/file?format=webm",
			body:     "",
			wantCode: http.StatusBadRequest,
			wantSub:  "cast",
		},
		{
			name:     "invalid from format",
			method:   http.MethodGet,
			path:     "/api/recordings?from=not-a-date",
			body:     "",
			wantCode: http.StatusBadRequest,
			wantSub:  "RFC3339",
		},
		{
			name:     "from after to",
			method:   http.MethodGet,
			path:     "/api/recordings?from=2026-05-10&to=2026-05-01",
			body:     "",
			wantCode: http.StatusBadRequest,
			wantSub:  "from は to",
		},
		{
			name:     "range too large",
			method:   http.MethodGet,
			path:     "/api/recordings?from=2026-01-01&to=2026-06-01",
			body:     "",
			wantCode: http.StatusBadRequest,
			wantSub:  "90 日",
		},
	})
}

func TestApp_ErrorMessage_LocalizedHandlers_Settings(t *testing.T) {
	_, router, sessID := japaneseLocalizedHandlerFixture(t)
	runLocalizedHandlerCases(t, sessID, router, []localizedHandlerCase{
		{
			name:     "invalid proto",
			method:   http.MethodPut,
			path:     "/api/settings/audit-forwarder",
			body:     `{"config":{"proto":"sctp"}}`,
			wantCode: http.StatusBadRequest,
			wantSub:  "プロトコル",
		},
	})
}

func TestApp_ErrorMessage_LocalizedHandlers_FileTransfers(t *testing.T) {
	_, router, sessID := japaneseLocalizedHandlerFixture(t)
	runLocalizedHandlerCases(t, sessID, router, []localizedHandlerCase{
		{
			name:     "invalid state",
			method:   http.MethodGet,
			path:     "/api/file-transfers?state=bogus",
			body:     "",
			wantCode: http.StatusBadRequest,
			wantSub:  "state",
		},
		{
			name:     "invalid direction",
			method:   http.MethodGet,
			path:     "/api/file-transfers?direction=foo",
			body:     "",
			wantCode: http.StatusBadRequest,
			wantSub:  "upload",
		},
		{
			name:     "invalid backend",
			method:   http.MethodGet,
			path:     "/api/file-transfers?backend=foo",
			body:     "",
			wantCode: http.StatusBadRequest,
			wantSub:  "remote",
		},
		{
			name:     "invalid cursor",
			method:   http.MethodGet,
			path:     "/api/file-transfers?after_cursor=not-base64!!!",
			body:     "",
			wantCode: http.StatusBadRequest,
			wantSub:  "after_id",
		},
	})
}

func TestApp_ErrorMessage_LocalizedHandlers_Password(t *testing.T) {
	_, router, sessID := japaneseLocalizedHandlerFixture(t)
	runLocalizedHandlerCases(t, sessID, router, []localizedHandlerCase{
		{
			name:     "empty",
			method:   http.MethodPost,
			path:     "/api/me/password",
			body:     `{"current_password":"admin","new_password":""}`,
			wantCode: http.StatusBadRequest,
			wantSub:  "必須",
		},
		{
			name:     "too short",
			method:   http.MethodPost,
			path:     "/api/me/password",
			body:     `{"current_password":"admin","new_password":"Ab1!"}`,
			wantCode: http.StatusBadRequest,
			wantSub:  "8 文字",
		},
		{
			name:     "no upper",
			method:   http.MethodPost,
			path:     "/api/me/password",
			body:     `{"current_password":"admin","new_password":"lowercase1!"}`,
			wantCode: http.StatusBadRequest,
			wantSub:  "大文字",
		},
		{
			name:     "no digit",
			method:   http.MethodPost,
			path:     "/api/me/password",
			body:     `{"current_password":"admin","new_password":"Abcdefgh!"}`,
			wantCode: http.StatusBadRequest,
			wantSub:  "数字",
		},
		{
			name:     "no special",
			method:   http.MethodPost,
			path:     "/api/me/password",
			body:     `{"current_password":"admin","new_password":"Abcdefg1"}`,
			wantCode: http.StatusBadRequest,
			wantSub:  "記号",
		},
	})
}

func TestApp_ErrorMessage_LocalizedHandlers_TagsAndUsers(t *testing.T) {
	_, router, sessID := japaneseLocalizedHandlerFixture(t)
	runLocalizedHandlerCases(t, sessID, router, []localizedHandlerCase{
		{
			name:     "groups invalid tag chars",
			method:   http.MethodPut,
			path:     "/api/groups/g1/tags",
			body:     `{"tags":["bad tag!"]}`,
			wantCode: http.StatusBadRequest,
			wantSub:  "タグ",
		},
		{
			name:     "createUser empty username",
			method:   http.MethodPost,
			path:     "/api/users",
			body:     `{"id":"u1","username":"","password":"Abcdef1!"}`,
			wantCode: http.StatusBadRequest,
			wantSub:  "ユーザー名",
		},
		{
			name:     "tag length invalid (empty)",
			method:   http.MethodPut,
			path:     "/api/users/admin/tags",
			body:     `{"tags":[""]}`,
			wantCode: http.StatusBadRequest,
			wantSub:  "タグ",
		},
	})
}
