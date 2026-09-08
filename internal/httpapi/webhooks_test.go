package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWebhooks_DeliveryFilteringAndSignature(t *testing.T) {
	t.Setenv(webhookAllowLoopback, "1")
	// The dispatcher is a package-level singleton; start from clean
	// delivery counters so the exact-count assertions below hold under
	// go test -count=N as well.
	globalWebhooks.mu.Lock()
	globalWebhooks.stats = map[string]*webhookStats{}
	globalWebhooks.mu.Unlock()
	var mu sync.Mutex
	var got []struct {
		event, sig string
		body       map[string]interface{}
	}
	fails := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/flaky" {
			mu.Lock()
			fails++
			n := fails
			mu.Unlock()
			if n == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
		}
		var body map[string]interface{}
		_ = json.Unmarshal(b, &body)
		mu.Lock()
		got = append(got, struct {
			event, sig string
			body       map[string]interface{}
		}{r.Header.Get("X-Vantyx-Event"), r.Header.Get("X-Vantyx-Signature"), body})
		mu.Unlock()
		if r.Header.Get("X-Vantyx-Signature") != "" {
			mac := hmac.New(sha256.New, []byte("s3cret"))
			mac.Write(b)
			if r.Header.Get("X-Vantyx-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
				t.Errorf("bad signature")
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	app := newTestApp(t)
	router := app.NewRouter()
	adminSess, _ := app.SessionStore.Create("admin")
	if _, err := app.UserStore.CreateUser("bob", "bob", "Password1!", "user"); err != nil {
		t.Fatal(err)
	}
	bobSess, _ := app.SessionStore.Create("bob")

	put := func(sessID string, endpoints []map[string]interface{}) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, jsonReq(t, http.MethodPut, "/api/settings/webhooks", map[string]interface{}{"endpoints": endpoints}, sessID))
		return w
	}
	if w := put(bobSess.ID, nil); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin: %d", w.Code)
	}
	// Validation: bad URL, no events, restricted host without the override.
	if w := put(adminSess.ID, []map[string]interface{}{{"name": "x", "url": "ftp://x", "events": []string{"*"}}}); w.Code != http.StatusBadRequest {
		t.Fatalf("bad scheme: %d", w.Code)
	}
	if w := put(adminSess.ID, []map[string]interface{}{{"name": "x", "url": srv.URL, "events": []string{}}}); w.Code != http.StatusBadRequest {
		t.Fatalf("no events: %d", w.Code)
	}
	// The httpapi TestMain allows restricted hosts globally; clear both
	// overrides to check that loopback targets are refused by default.
	prevRestricted := os.Getenv("VANTYX_ALLOW_RESTRICTED_HOSTS")
	t.Setenv("VANTYX_ALLOW_RESTRICTED_HOSTS", "")
	t.Setenv(webhookAllowLoopback, "")
	if w := put(adminSess.ID, []map[string]interface{}{{"name": "x", "url": srv.URL, "events": []string{"*"}}}); w.Code != http.StatusBadRequest {
		t.Fatalf("loopback should be refused: %d %s", w.Code, w.Body.String())
	}
	t.Setenv("VANTYX_ALLOW_RESTRICTED_HOSTS", prevRestricted)
	t.Setenv(webhookAllowLoopback, "1")

	w := put(adminSess.ID, []map[string]interface{}{
		{"id": "generic", "name": "Generic", "url": srv.URL + "/generic", "secret": "s3cret", "format": "generic", "events": []string{"access_request_*", "user_role_update"}, "enabled": true},
		{"id": "slack", "name": "Slack", "url": srv.URL + "/slack", "format": "slack", "events": []string{"user_role_update"}, "enabled": true},
		{"id": "off", "name": "Off", "url": srv.URL + "/off", "events": []string{"*"}, "enabled": false},
		{"id": "flaky", "name": "Flaky", "url": srv.URL + "/flaky", "events": []string{"user_role_update"}, "enabled": true},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "s3cret") || !strings.Contains(w.Body.String(), `"has_secret":true`) {
		t.Fatalf("secret leaked or has_secret missing: %s", w.Body.String())
	}

	audit("user_role_update", auditFields{"user_id": "admin", "target_id": "bob", "to": "admin"})
	audit("access_request_created", auditFields{"user_id": "bob", "group_id": "net"})
	audit("login_failed", auditFields{"username_hash": "x"}) // nobody subscribed
	audit("http_request", auditFields{"path": "/api/x"})     // never sent implicitly

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 4 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	snapshot := append(got[:0:0], got...)
	failCount := fails
	mu.Unlock()
	events := map[string]int{}
	sawSlack, sawSigned := false, false
	for _, g := range snapshot {
		events[g.event]++
		if txt, ok := g.body["text"].(string); ok && strings.Contains(txt, "user_role_update") {
			sawSlack = true
		}
		if g.sig != "" {
			sawSigned = true
			if g.body["event"] != g.event || g.body["fields"] == nil {
				t.Fatalf("generic body = %v", g.body)
			}
		}
	}
	// user_role_update -> generic + slack + flaky(retry) = 3; access_request_created -> generic = 1
	if events["user_role_update"] != 3 || events["access_request_created"] != 1 || events["login_failed"] != 0 || events["http_request"] != 0 {
		t.Fatalf("deliveries = %v (got %d)", events, len(snapshot))
	}
	if !sawSlack || !sawSigned {
		t.Fatalf("slack=%v signed=%v", sawSlack, sawSigned)
	}
	if failCount < 2 {
		t.Fatalf("flaky endpoint was not retried (fails=%d)", failCount)
	}

	// Stats and test endpoint.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/settings/webhooks", nil, adminSess.ID))
	if !strings.Contains(w.Body.String(), `"sent":2`) {
		t.Fatalf("stats: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/settings/webhooks/generic/test", nil, adminSess.ID))
	if w.Code != http.StatusOK || decodeJSON(t, w)["ok"] != true {
		t.Fatalf("test: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/settings/webhooks/nope/test", nil, adminSess.ID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("test unknown: %d", w.Code)
	}
	// Omitting the secret on re-save keeps it.
	w = put(adminSess.ID, []map[string]interface{}{{"id": "generic", "name": "Generic", "url": srv.URL + "/generic", "format": "generic", "events": []string{"*"}, "enabled": true}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"has_secret":true`) {
		t.Fatalf("secret not kept: %d %s", w.Code, w.Body.String())
	}
	if !webhookEventMatches([]string{"access_request_*"}, "access_request_denied") || webhookEventMatches([]string{"access_request_*"}, "login_failed") {
		t.Fatal("glob matching broken")
	}
}

// The outbound client re-checks the *resolved* address and never follows
// redirects, so DNS names or 3xx hops cannot reach loopback / link-local /
// metadata addresses that the URL validation refuses.
func TestWebhooks_DialRefusesResolvedRestrictedAddressesAndRedirects(t *testing.T) {
	prevRestricted := os.Getenv("VANTYX_ALLOW_RESTRICTED_HOSTS")
	t.Setenv("VANTYX_ALLOW_RESTRICTED_HOSTS", "")
	t.Setenv(webhookAllowLoopback, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, addr := range []string{"localhost:80", "127.0.0.1:80", "169.254.169.254:80", "[::1]:80"} {
		if _, err := webhookDialContext(ctx, "tcp", addr); err == nil || !errors.Is(err, errWebhookURL) {
			t.Errorf("dial %s: err = %v, want restricted", addr, err)
		}
	}
	t.Setenv("VANTYX_ALLOW_RESTRICTED_HOSTS", prevRestricted)
	t.Setenv(webhookAllowLoopback, "1")

	// A redirecting endpoint counts as a failed delivery: the hop is not
	// followed, so the (would-be internal) destination never sees a request.
	hits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; w.WriteHeader(http.StatusNoContent) }))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()
	ep := webhookEndpoint{ID: "r", Name: "r", URL: redirector.URL, Format: "generic", Events: []string{"*"}, Enabled: true}
	status, err := globalWebhooks.deliver(ctx, ep, AuditEntry{Time: time.Now(), Event: "webhook_test", Fields: auditFields{}})
	if err == nil || hits != 0 {
		t.Fatalf("redirect: status=%d err=%v hits=%d; want error and no hit", status, err, hits)
	}
}

func TestWebhooks_SlackEscaping(t *testing.T) {
	body, err := webhookBody(webhookEndpoint{Format: "slack"}, AuditEntry{Time: time.Now(), Event: "x", Fields: auditFields{"reason": "<!channel> & <https://evil|click>"}})
	if err != nil {
		t.Fatal(err)
	}
	var msg map[string]string
	if err := json.Unmarshal(body, &msg); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg["text"], "<!channel>") || !strings.Contains(msg["text"], "&lt;!channel&gt; &amp; &lt;https://evil|click&gt;") {
		t.Fatalf("slack text not escaped: %q", msg["text"])
	}
}
