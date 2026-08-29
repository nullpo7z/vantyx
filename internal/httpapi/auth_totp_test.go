package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func totpCodeFor(t *testing.T, secretB32 string) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secretB32, time.Now().UTC(), totp.ValidateOpts{
		Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	return code
}

func jsonReq(t *testing.T, method, path string, body interface{}, sessionID string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessionID, Path: "/"})
	}
	return req
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return m
}

func sessionCookie(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == "vantyx_session" {
			return c.Value
		}
	}
	return ""
}

// enrolAdminTOTP enables TOTP for admin through the public API and
// returns the secret plus the recovery codes.
func enrolAdminTOTP(t *testing.T, app *App, router http.Handler, sessionID string) (string, []string) {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/me/totp/setup", nil, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	setup := decodeJSON(t, w)
	secretB32, _ := setup["secret"].(string)
	if secretB32 == "" || !strings.HasPrefix(setup["qr_png"].(string), "data:image/png;base64,") {
		t.Fatalf("setup response missing secret/qr: %v", setup)
	}
	if !strings.Contains(setup["otpauth_url"].(string), "Vantyx") {
		t.Fatalf("otpauth_url without issuer: %v", setup["otpauth_url"])
	}
	// Enrolment is not enforced until confirmed.
	if app.TOTPStore.Enabled(context.Background(), "admin") {
		t.Fatal("TOTP enforced before confirmation")
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/me/totp/confirm", map[string]string{"code": totpCodeFor(t, secretB32)}, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	raw, _ := resp["recovery_codes"].([]interface{})
	codes := make([]string, 0, len(raw))
	for _, c := range raw {
		codes = append(codes, c.(string))
	}
	if len(codes) != 8 {
		t.Fatalf("got %d recovery codes, want 8", len(codes))
	}
	return secretB32, codes
}

func TestTOTP_LoginRequiresSecondFactor(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	secretB32, recovery := enrolAdminTOTP(t, app, router, sess.ID)

	// /api/me reports the factor.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/me", nil, sess.ID))
	if me := decodeJSON(t, w); me["totp_enabled"] != true {
		t.Fatalf("/api/me totp_enabled = %v, want true", me["totp_enabled"])
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/me/totp", nil, sess.ID))
	if st := decodeJSON(t, w); st["enabled"] != true || st["recovery_codes_left"] != float64(8) {
		t.Fatalf("/api/me/totp = %v", st)
	}

	// Password alone no longer yields a session.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login", map[string]string{"username": "admin", "password": "Admin123!"}, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	if sessionCookie(w) != "" {
		t.Fatal("session cookie issued before the second factor")
	}
	login := decodeJSON(t, w)
	if login["mfa_required"] != true {
		t.Fatalf("login response = %v, want mfa_required", login)
	}
	tok, _ := login["mfa_token"].(string)
	if tok == "" {
		t.Fatal("mfa_token missing")
	}

	// Wrong code: 401, token still valid.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login/totp", map[string]string{"mfa_token": tok, "code": "000000"}, ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code: %d %s", w.Code, w.Body.String())
	}
	// Bogus token: 401.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login/totp", map[string]string{"mfa_token": "nope", "code": totpCodeFor(t, secretB32)}, ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bogus token: %d %s", w.Code, w.Body.String())
	}

	// Right code: session issued, token consumed.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login/totp", map[string]string{"mfa_token": tok, "code": totpCodeFor(t, secretB32)}, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("totp login: %d %s", w.Code, w.Body.String())
	}
	if sessionCookie(w) == "" {
		t.Fatal("no session cookie after TOTP")
	}
	if got := decodeJSON(t, w)["user_id"]; got != "admin" {
		t.Fatalf("user_id = %v", got)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login/totp", map[string]string{"mfa_token": tok, "code": totpCodeFor(t, secretB32)}, ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("replayed mfa_token: %d, want 401", w.Code)
	}

	// Recovery code works once.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login", map[string]string{"username": "admin", "password": "Admin123!"}, ""))
	tok2, _ := decodeJSON(t, w)["mfa_token"].(string)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login/totp", map[string]string{"mfa_token": tok2, "code": recovery[0]}, ""))
	if w.Code != http.StatusOK || sessionCookie(w) == "" {
		t.Fatalf("recovery login: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login", map[string]string{"username": "admin", "password": "Admin123!"}, ""))
	tok3, _ := decodeJSON(t, w)["mfa_token"].(string)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login/totp", map[string]string{"mfa_token": tok3, "code": recovery[0]}, ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("reused recovery code: %d, want 401", w.Code)
	}
}

func TestTOTP_MFATokenDiscardedAfterTooManyFailures(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	secretB32, _ := enrolAdminTOTP(t, app, router, sess.ID)
	// The login limiter would kick in first with its own threshold; take
	// it out of the picture so the per-token cap is what is exercised.
	app.LoginRateLimiter = nil

	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login", map[string]string{"username": "admin", "password": "Admin123!"}, ""))
	tok, _ := decodeJSON(t, w)["mfa_token"].(string)
	for i := 0; i < mfaMaxFailures; i++ {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login/totp", map[string]string{"mfa_token": tok, "code": "000000"}, ""))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login/totp", map[string]string{"mfa_token": tok, "code": totpCodeFor(t, secretB32)}, ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("correct code after %d failures: %d, want 401 (token discarded)", mfaMaxFailures, w.Code)
	}
}

func TestTOTP_DisableRequiresPasswordAndAdminReset(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	enrolAdminTOTP(t, app, router, sess.ID)

	// Setup while enabled is refused (must disable first).
	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/me/totp/setup", nil, sess.ID))
	if w.Code != http.StatusConflict {
		t.Fatalf("setup while enabled: %d, want 409", w.Code)
	}
	// Wrong password: 403 and still enabled.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/me/totp", map[string]string{"password": "wrong"}, sess.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("disable with wrong password: %d, want 403", w.Code)
	}
	if !app.TOTPStore.Enabled(context.Background(), "admin") {
		t.Fatal("TOTP disabled despite wrong password")
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/me/totp", map[string]string{"password": "Admin123!"}, sess.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	if app.TOTPStore.Enabled(context.Background(), "admin") {
		t.Fatal("TOTP still enabled after disable")
	}
	// Password login is single-step again.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login", map[string]string{"username": "admin", "password": "Admin123!"}, ""))
	if w.Code != http.StatusOK || sessionCookie(w) == "" {
		t.Fatalf("login after disable: %d, cookie=%q", w.Code, sessionCookie(w))
	}

	// Admin reset for another user.
	if _, err := app.UserStore.CreateUser("bob", "bob", "Password1!", "user"); err != nil {
		t.Fatalf("create bob: %v", err)
	}
	bobSess, _ := app.SessionStore.Create("bob")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/me/totp/setup", nil, bobSess.ID))
	secretB32 := decodeJSON(t, w)["secret"].(string)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/me/totp/confirm", map[string]string{"code": totpCodeFor(t, secretB32)}, bobSess.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("bob confirm: %d %s", w.Code, w.Body.String())
	}
	// Non-admin cannot reset anyone.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/users/admin/totp", nil, bobSess.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin reset: %d, want 403", w.Code)
	}
	// Admin list shows the flag; reset clears it.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/users", nil, sess.ID))
	if !strings.Contains(w.Body.String(), `"id":"bob"`) || !strings.Contains(w.Body.String(), `"totp_enabled":true`) {
		t.Fatalf("user list lacks bob's totp flag: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/users/bob/totp", nil, sess.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("admin reset: %d %s", w.Code, w.Body.String())
	}
	if app.TOTPStore.Enabled(context.Background(), "bob") {
		t.Fatal("bob still has TOTP after admin reset")
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/users/bob/totp", nil, sess.ID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("reset twice: %d, want 404", w.Code)
	}
}

func TestTOTP_EndpointsRequireSession(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/me/totp"},
		{http.MethodPost, "/api/me/totp/setup"},
		{http.MethodPost, "/api/me/totp/confirm"},
		{http.MethodDelete, "/api/me/totp"},
		{http.MethodDelete, "/api/users/admin/totp"},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, jsonReq(t, tc.method, tc.path, map[string]string{}, ""))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without session: %d, want 401", tc.method, tc.path, w.Code)
		}
	}
}

func TestAuthMethods_PublicAndOIDCOffByDefault(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("methods: %d", w.Code)
	}
	m := decodeJSON(t, w)
	if m["password"] != true {
		t.Fatalf("password = %v", m["password"])
	}
	oidcInfo, _ := m["oidc"].(map[string]interface{})
	if oidcInfo == nil || oidcInfo["enabled"] != false {
		t.Fatalf("oidc = %v, want enabled=false", m["oidc"])
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("oidc login while disabled: %d, want 404", w.Code)
	}
}
