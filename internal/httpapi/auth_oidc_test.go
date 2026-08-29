package httpapi

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/auth"
)

// fakeIdP is a minimal OpenID Provider: discovery, JWKS and a token
// endpoint that mints an RS256 ID token for whatever claims the test
// asked for. The authorization endpoint is never called (the test reads
// the redirect the app produces instead of following it).
type fakeIdP struct {
	srv *httptest.Server
	key *rsa.PrivateKey

	// discoveryPath is where the well-known document is served ("" =
	// at the issuer root); issuer is the value the document and ID
	// tokens carry (defaults to srv.URL).
	discoveryPath string
	issuer        string

	mu     sync.Mutex
	claims map[string]interface{} // extra claims for the next ID token
	nonce  string
	tokens int
	verif  string // last code_verifier seen at the token endpoint
}

func newFakeIdP(t *testing.T) *fakeIdP {
	return newFakeIdPAt(t, "")
}

// newFakeIdPAt serves the discovery document under discoveryPath (for
// example "/cdn-cgi/access/sso/oidc/cid") while the issuer stays the
// server root, like Cloudflare Access does.
func newFakeIdPAt(t *testing.T, discoveryPath string) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	p := &fakeIdP{key: key, claims: map[string]interface{}{}, discoveryPath: discoveryPath}
	mux := http.NewServeMux()
	mux.HandleFunc(discoveryPath+"/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                                p.issuer,
			"authorization_endpoint":                p.srv.URL + "/authorize",
			"token_endpoint":                        p.srv.URL + "/token",
			"jwks_uri":                              p.srv.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		pub := key.Public().(*rsa.PublicKey)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"keys": []map[string]string{{
				"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
				"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		p.mu.Lock()
		p.tokens++
		p.verif = r.PostForm.Get("code_verifier")
		claims := map[string]interface{}{
			"iss":   p.issuer,
			"aud":   "vantyx-test",
			"exp":   time.Now().Add(5 * time.Minute).Unix(),
			"iat":   time.Now().Unix(),
			"nonce": p.nonce,
		}
		for k, v := range p.claims {
			claims[k] = v
		}
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "at",
			"token_type":   "Bearer",
			"expires_in":   300,
			"id_token":     p.sign(t, claims),
		})
	})
	p.srv = httptest.NewServer(mux)
	p.issuer = p.srv.URL
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeIdP) sign(t *testing.T, claims map[string]interface{}) string {
	t.Helper()
	hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "k1"})
	body, _ := json.Marshal(claims)
	signing := base64.RawURLEncoding.EncodeToString(hdr) + "." + base64.RawURLEncoding.EncodeToString(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (p *fakeIdP) setClaims(c map[string]interface{}) {
	p.mu.Lock()
	p.claims = c
	p.mu.Unlock()
}

func (p *fakeIdP) setNonce(n string) {
	p.mu.Lock()
	p.nonce = n
	p.mu.Unlock()
}

func newOIDCTestApp(t *testing.T, idp *fakeIdP, autoCreate bool) (*App, http.Handler) {
	t.Helper()
	app := newTestApp(t)
	app.oidc = &oidcService{
		cfg: &oidcConfig{
			Issuer:        idp.srv.URL,
			ClientID:      "vantyx-test",
			ClientSecret:  "secret",
			RedirectURL:   "http://vantyx.example/api/auth/oidc/callback",
			Scopes:        []string{"openid", "profile", "email"},
			UsernameClaim: "preferred_username",
			AutoCreate:    autoCreate,
			DisplayName:   "Test IdP",
		},
		states: make(map[string]*oidcPending),
	}
	return app, app.NewRouter()
}

// startOIDCLogin hits /api/auth/oidc/login and returns the state cookie
// plus the state and nonce embedded in the authorization redirect.
func startOIDCLogin(t *testing.T, router http.Handler, next string) (cookie *http.Cookie, state, nonce string) {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login?next="+url.QueryEscape(next), nil))
	if w.Code != http.StatusFound {
		t.Fatalf("oidc login: %d %s", w.Code, w.Body.String())
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	q := loc.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("PKCE missing from authorization URL: %s", loc)
	}
	if q.Get("redirect_uri") != "http://vantyx.example/api/auth/oidc/callback" || q.Get("client_id") != "vantyx-test" {
		t.Fatalf("unexpected authorization URL: %s", loc)
	}
	if !strings.Contains(q.Get("scope"), "openid") {
		t.Fatalf("scope %q lacks openid", q.Get("scope"))
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == oidcStateCookie {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != q.Get("state") {
		t.Fatalf("state cookie %v does not match state param %q", cookie, q.Get("state"))
	}
	return cookie, q.Get("state"), q.Get("nonce")
}

func oidcCallback(t *testing.T, router http.Handler, cookie *http.Cookie, state string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state="+url.QueryEscape(state), nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestAuthMethods_ReportsOIDCWhenConfigured(t *testing.T) {
	idp := newFakeIdP(t)
	_, router := newOIDCTestApp(t, idp, false)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil))
	m := decodeJSON(t, w)
	o, _ := m["oidc"].(map[string]interface{})
	if o == nil || o["enabled"] != true || o["display_name"] != "Test IdP" {
		t.Fatalf("oidc = %v", m["oidc"])
	}
}

func TestOIDC_LoginAutoCreatesAndLinksUser(t *testing.T) {
	idp := newFakeIdP(t)
	app, router := newOIDCTestApp(t, idp, true)
	idp.setClaims(map[string]interface{}{"sub": "sub-123", "preferred_username": "Alice", "email": "alice@example.com"})

	cookie, state, nonce := startOIDCLogin(t, router, "/terminal?target_id=demo")
	idp.setNonce(nonce)
	w := oidcCallback(t, router, cookie, state)
	if w.Code != http.StatusFound {
		t.Fatalf("callback: %d %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/terminal?target_id=demo" {
		t.Fatalf("redirect = %q, want the next path", loc)
	}
	sessID := sessionCookie(w)
	if sessID == "" {
		t.Fatal("no session cookie after OIDC login")
	}
	if idp.verif == "" {
		t.Fatal("token endpoint did not receive a PKCE code_verifier")
	}

	// The user exists with role=user and is linked to (issuer, sub).
	sess, err := app.SessionStore.Get(sessID)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	u, err := app.UserStore.GetByID(sess.UserID)
	if err != nil || u == nil {
		t.Fatalf("user %q: %v", sess.UserID, err)
	}
	if u.Username != "Alice" || u.Role != auth.RoleUser {
		t.Fatalf("auto-created user = %+v", u)
	}
	if uid, err := app.OIDCLinks.Lookup(context.Background(), idp.srv.URL, "sub-123"); err != nil || uid != u.ID {
		t.Fatalf("link lookup = %q, %v; want %q", uid, err, u.ID)
	}
	// The random password must not be guessable / usable.
	if _, err := app.UserStore.Authenticate("Alice", ""); err == nil {
		t.Fatal("empty password accepted for OIDC user")
	}

	// A second login with the same subject but a changed username still
	// resolves through the link, not by creating another account.
	idp.setClaims(map[string]interface{}{"sub": "sub-123", "preferred_username": "alice-renamed"})
	cookie, state, nonce = startOIDCLogin(t, router, "/")
	idp.setNonce(nonce)
	w = oidcCallback(t, router, cookie, state)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("second callback: %d %q", w.Code, w.Header().Get("Location"))
	}
	sess2, _ := app.SessionStore.Get(sessionCookie(w))
	if sess2 == nil || sess2.UserID != u.ID {
		t.Fatalf("second login mapped to %v, want %q", sess2, u.ID)
	}
	users, _ := app.UserStore.ListUsers(100, 0)
	for _, x := range users {
		if x.Username == "alice-renamed" {
			t.Fatal("renamed subject created a duplicate account")
		}
	}

	// The state is single-use.
	w = oidcCallback(t, router, cookie, state)
	if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "oidc_error=state") {
		t.Fatalf("replayed state: %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestOIDC_LinksExistingUserByUsername(t *testing.T) {
	idp := newFakeIdP(t)
	app, router := newOIDCTestApp(t, idp, false)
	if _, err := app.UserStore.CreateUser("bob", "Bob", "Password1!", "admin"); err != nil {
		t.Fatalf("create bob: %v", err)
	}
	// Case-insensitive username match, no auto-create needed.
	idp.setClaims(map[string]interface{}{"sub": "sub-bob", "preferred_username": "bob"})
	cookie, state, nonce := startOIDCLogin(t, router, "/")
	idp.setNonce(nonce)
	w := oidcCallback(t, router, cookie, state)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("callback: %d %q", w.Code, w.Header().Get("Location"))
	}
	sess, _ := app.SessionStore.Get(sessionCookie(w))
	if sess == nil || sess.UserID != "bob" {
		t.Fatalf("session user = %v, want bob", sess)
	}
	if uid, err := app.OIDCLinks.Lookup(context.Background(), idp.srv.URL, "sub-bob"); err != nil || uid != "bob" {
		t.Fatalf("link = %q, %v", uid, err)
	}
	// Falls back to the email claim when the username claim is absent.
	if _, err := app.UserStore.CreateUser("carol", "carol@example.com", "Password1!", "user"); err != nil {
		t.Fatalf("create carol: %v", err)
	}
	idp.setClaims(map[string]interface{}{"sub": "sub-carol", "email": "carol@example.com"})
	cookie, state, nonce = startOIDCLogin(t, router, "/")
	idp.setNonce(nonce)
	w = oidcCallback(t, router, cookie, state)
	sess, _ = app.SessionStore.Get(sessionCookie(w))
	if sess == nil || sess.UserID != "carol" {
		t.Fatalf("email fallback: session user = %v, want carol", sess)
	}
}

func TestOIDC_UnknownUserWithoutAutoCreateIsRejected(t *testing.T) {
	idp := newFakeIdP(t)
	app, router := newOIDCTestApp(t, idp, false)
	idp.setClaims(map[string]interface{}{"sub": "sub-x", "preferred_username": "stranger"})
	cookie, state, nonce := startOIDCLogin(t, router, "/")
	idp.setNonce(nonce)
	w := oidcCallback(t, router, cookie, state)
	if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "oidc_error=not_provisioned") {
		t.Fatalf("callback: %d %q", w.Code, w.Header().Get("Location"))
	}
	if sessionCookie(w) != "" {
		t.Fatal("session issued for unprovisioned user")
	}
	users, _ := app.UserStore.ListUsers(100, 0)
	for _, u := range users {
		if u.Username == "stranger" {
			t.Fatal("user created although auto-create is off")
		}
	}
}

func TestOIDC_RejectsBadStateNonceAndAudience(t *testing.T) {
	idp := newFakeIdP(t)
	_, router := newOIDCTestApp(t, idp, true)
	idp.setClaims(map[string]interface{}{"sub": "sub-1", "preferred_username": "dave"})

	// Missing cookie.
	_, state, nonce := startOIDCLogin(t, router, "/")
	idp.setNonce(nonce)
	w := oidcCallback(t, router, nil, state)
	if !strings.Contains(w.Header().Get("Location"), "oidc_error=state") {
		t.Fatalf("no cookie: %q", w.Header().Get("Location"))
	}
	// Cookie / state mismatch (CSRF).
	cookie, _, nonce := startOIDCLogin(t, router, "/")
	idp.setNonce(nonce)
	w = oidcCallback(t, router, cookie, "attacker-state")
	if !strings.Contains(w.Header().Get("Location"), "oidc_error=state") {
		t.Fatalf("state mismatch: %q", w.Header().Get("Location"))
	}
	// Nonce mismatch (replayed ID token).
	cookie, state, _ = startOIDCLogin(t, router, "/")
	idp.setNonce("stale-nonce")
	w = oidcCallback(t, router, cookie, state)
	if !strings.Contains(w.Header().Get("Location"), "oidc_error=token") || sessionCookie(w) != "" {
		t.Fatalf("nonce mismatch: %q cookie=%q", w.Header().Get("Location"), sessionCookie(w))
	}
	// Wrong audience.
	idp.setClaims(map[string]interface{}{"sub": "sub-1", "preferred_username": "dave", "aud": "someone-else"})
	cookie, state, nonce = startOIDCLogin(t, router, "/")
	idp.setNonce(nonce)
	w = oidcCallback(t, router, cookie, state)
	if !strings.Contains(w.Header().Get("Location"), "oidc_error=token") || sessionCookie(w) != "" {
		t.Fatalf("aud mismatch: %q", w.Header().Get("Location"))
	}
	// IdP-side denial.
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?error=access_denied&state=x", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if !strings.Contains(w.Header().Get("Location"), "oidc_error=denied") {
		t.Fatalf("idp error: %q", w.Header().Get("Location"))
	}
}

// Cloudflare Access style: discovery lives under a per-client path, the
// issuer in the document / ID token is the bare team domain.
func TestOIDC_DiscoveryURLDistinctFromIssuer(t *testing.T) {
	idp := newFakeIdPAt(t, "/cdn-cgi/access/sso/oidc/cid")
	idp.setClaims(map[string]interface{}{"sub": "cf-1", "email": "eve@example.com", "name": "Eve"})

	// Without the discovery URL the well-known document is not at the issuer.
	app, router := newOIDCTestApp(t, idp, true)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login", nil))
	if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "oidc_error=provider_unavailable") {
		t.Fatalf("login without discovery url: %d %q", w.Code, w.Header().Get("Location"))
	}

	app.oidc.cfg.DiscoveryURL = idp.srv.URL + "/cdn-cgi/access/sso/oidc/cid"
	app.oidc.cfg.UsernameClaim = "email"
	cookie, state, nonce := startOIDCLogin(t, router, "/")
	idp.setNonce(nonce)
	w = oidcCallback(t, router, cookie, state)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("callback: %d %q", w.Code, w.Header().Get("Location"))
	}
	sess, _ := app.SessionStore.Get(sessionCookie(w))
	if sess == nil {
		t.Fatal("no session")
	}
	u, _ := app.UserStore.GetByID(sess.UserID)
	if u == nil || u.Username != "eve@example.com" {
		t.Fatalf("user = %+v, want username from email claim", u)
	}
	// The link records the real issuer (team domain), not the discovery URL.
	if uid, err := app.OIDCLinks.Lookup(context.Background(), idp.srv.URL, "cf-1"); err != nil || uid != u.ID {
		t.Fatalf("link lookup = %q, %v", uid, err)
	}
	// An ID token whose iss is the discovery URL (wrong issuer) is rejected.
	idp.issuer = idp.srv.URL + "/cdn-cgi/access/sso/oidc/cid"
	cookie, state, nonce = startOIDCLogin(t, router, "/")
	idp.setNonce(nonce)
	w = oidcCallback(t, router, cookie, state)
	if !strings.Contains(w.Header().Get("Location"), "oidc_error=token") {
		t.Fatalf("issuer mismatch accepted: %q", w.Header().Get("Location"))
	}
}

func TestOIDC_NextPathIsSameOriginOnly(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "/",
		"/recordings":            "/recordings",
		"/terminal?x=1#f":        "/terminal?x=1#f",
		"//evil.example/":        "/",
		"/\\evil.example":        "/",
		"https://evil.example/":  "/",
		"javascript:alert(1)":    "/",
		"relative/path":          "/",
		"/ok?next=//evil":        "/ok?next=//evil",
		"  /trimmed  ":           "/trimmed",
		"/%2F%2Fevil.example/":   "/%2F%2Fevil.example/",
		"http:/evil.example":     "/",
		"/path\nSet-Cookie: x=y": "/",
	} {
		if got := safeNextPath(in); got != want {
			t.Errorf("safeNextPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOIDCConfigFromEnv(t *testing.T) {
	t.Setenv("VANTYX_OIDC_ISSUER", "")
	t.Setenv("VANTYX_OIDC_CLIENT_ID", "")
	if oidcConfigFromEnv() != nil {
		t.Fatal("config without issuer/client id should be nil")
	}
	t.Setenv("VANTYX_OIDC_ISSUER", "https://idp.example/realms/x")
	t.Setenv("VANTYX_OIDC_CLIENT_ID", "cid")
	t.Setenv("VANTYX_OIDC_SCOPES", "profile,groups")
	t.Setenv("VANTYX_OIDC_AUTO_CREATE_USERS", "true")
	t.Setenv("VANTYX_OIDC_USERNAME_CLAIM", "")
	t.Setenv("VANTYX_OIDC_DISPLAY_NAME", "")
	t.Setenv("VANTYX_OIDC_DISCOVERY_URL", "https://team.cloudflareaccess.com/cdn-cgi/access/sso/oidc/cid/")
	cfg := oidcConfigFromEnv()
	if cfg == nil {
		t.Fatal("config nil")
	}
	if !cfg.AutoCreate || cfg.UsernameClaim != "preferred_username" || cfg.DisplayName != "SSO" {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if cfg.DiscoveryURL != "https://team.cloudflareaccess.com/cdn-cgi/access/sso/oidc/cid" {
		t.Fatalf("DiscoveryURL = %q (trailing slash should be trimmed)", cfg.DiscoveryURL)
	}
	if strings.Join(cfg.Scopes, " ") != "openid profile groups" {
		t.Fatalf("scopes = %v; openid must be prepended", cfg.Scopes)
	}
}
