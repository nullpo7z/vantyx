package httpapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// softAuthenticator is a minimal WebAuthn authenticator (ES256, "none"
// attestation) so the registration and login ceremonies can be exercised
// end to end without a browser.
type softAuthenticator struct {
	key    *ecdsa.PrivateKey
	credID []byte
	count  uint32
	rpID   string
	origin string
}

func newSoftAuthenticator(t *testing.T, rpID, origin string) *softAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	credID := make([]byte, 16)
	_, _ = rand.Read(credID)
	return &softAuthenticator{key: key, credID: credID, rpID: rpID, origin: origin}
}

func (s *softAuthenticator) clientData(typ, challengeB64 string) []byte {
	b, _ := json.Marshal(map[string]interface{}{"type": typ, "challenge": challengeB64, "origin": s.origin})
	return b
}

func (s *softAuthenticator) authData(flags byte, attested bool) []byte {
	rpHash := sha256.Sum256([]byte(s.rpID))
	out := append([]byte{}, rpHash[:]...)
	out = append(out, flags)
	cnt := make([]byte, 4)
	binary.BigEndian.PutUint32(cnt, s.count)
	out = append(out, cnt...)
	if attested {
		out = append(out, make([]byte, 16)...) // AAGUID
		l := make([]byte, 2)
		binary.BigEndian.PutUint16(l, uint16(len(s.credID)))
		out = append(out, l...)
		out = append(out, s.credID...)
		pub := s.key.PublicKey
		cose := map[int]interface{}{1: 2, 3: -7, -1: 1, -2: pub.X.FillBytes(make([]byte, 32)), -3: pub.Y.FillBytes(make([]byte, 32))}
		enc, _ := cbor.Marshal(cose)
		out = append(out, enc...)
	}
	return out
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// register answers a creation options object.
func (s *softAuthenticator) register(t *testing.T, options map[string]interface{}) map[string]interface{} {
	t.Helper()
	pk := options["publicKey"].(map[string]interface{})
	challenge := pk["challenge"].(string)
	cd := s.clientData("webauthn.create", challenge)
	ad := s.authData(0x45, true) // UP | UV | AT
	att, _ := cbor.Marshal(map[string]interface{}{"fmt": "none", "attStmt": map[string]interface{}{}, "authData": ad})
	return map[string]interface{}{
		"id": b64(s.credID), "rawId": b64(s.credID), "type": "public-key",
		"response": map[string]interface{}{"clientDataJSON": b64(cd), "attestationObject": b64(att), "transports": []string{"internal"}},
	}
}

// assert answers a request options object.
func (s *softAuthenticator) assert(t *testing.T, options map[string]interface{}) map[string]interface{} {
	t.Helper()
	pk := options["publicKey"].(map[string]interface{})
	challenge := pk["challenge"].(string)
	cd := s.clientData("webauthn.get", challenge)
	s.count++
	ad := s.authData(0x05, false) // UP | UV
	cdHash := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), cdHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, s.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return map[string]interface{}{
		"id": b64(s.credID), "rawId": b64(s.credID), "type": "public-key",
		"response": map[string]interface{}{"clientDataJSON": b64(cd), "authenticatorData": b64(ad), "signature": b64(sig), "userHandle": b64([]byte("admin"))},
	}
}

func TestWebAuthn_RegisterAndLoginAsSecondFactor(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	adminSess, _ := app.SessionStore.Create("admin")
	const host = "example.com"
	auth := newSoftAuthenticator(t, host, "http://"+host)

	do := func(method, path string, body interface{}, sessID string) *httptest.ResponseRecorder {
		req := jsonReq(t, method, path, body, sessID)
		req.Host = host
		req.Header.Set("Origin", "http://"+host)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// Registration: begin -> options; finish with a wrong challenge fails,
	// then a proper answer succeeds and the passkey is listed.
	w := do(http.MethodPost, "/api/me/webauthn/register/begin", nil, adminSess.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("register begin: %d %s", w.Code, w.Body.String())
	}
	opts := decodeJSON(t, w)
	if pk, _ := opts["publicKey"].(map[string]interface{}); pk == nil || pk["challenge"] == "" {
		t.Fatalf("options = %v", opts)
	}
	bad := auth.register(t, opts)
	bad["response"].(map[string]interface{})["clientDataJSON"] = b64(auth.clientData("webauthn.create", b64([]byte("wrong-challenge"))))
	if w := do(http.MethodPost, "/api/me/webauthn/register/finish", map[string]interface{}{"name": "bad", "credential": bad}, adminSess.ID); w.Code != http.StatusBadRequest {
		t.Fatalf("bad challenge accepted: %d %s", w.Code, w.Body.String())
	}
	// The failed attempt consumed the pending session: begin again.
	w = do(http.MethodPost, "/api/me/webauthn/register/begin", nil, adminSess.ID)
	opts = decodeJSON(t, w)
	w = do(http.MethodPost, "/api/me/webauthn/register/finish", map[string]interface{}{"name": "YubiKey", "credential": auth.register(t, opts)}, adminSess.ID)
	if w.Code != http.StatusCreated {
		t.Fatalf("register finish: %d %s", w.Code, w.Body.String())
	}
	if decodeJSON(t, w)["name"] != "YubiKey" {
		t.Fatalf("created = %s", w.Body.String())
	}
	w = do(http.MethodGet, "/api/me/webauthn", nil, adminSess.ID)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"YubiKey"`) {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	w = do(http.MethodGet, "/api/me", nil, adminSess.ID)
	if decodeJSON(t, w)["passkeys"] != float64(1) {
		t.Fatalf("/api/me passkeys: %s", w.Body.String())
	}

	// Login now requires a second factor offering webauthn (no TOTP).
	w = do(http.MethodPost, "/api/login", map[string]string{"username": "admin", "password": "Admin123!"}, "")
	login := decodeJSON(t, w)
	if login["mfa_required"] != true || sessionCookie(w) != "" {
		t.Fatalf("login = %v", login)
	}
	methods, _ := json.Marshal(login["methods"])
	if string(methods) != `["webauthn"]` {
		t.Fatalf("methods = %s", methods)
	}
	tok := login["mfa_token"].(string)
	// TOTP path is refused (not enabled), webauthn path works.
	if w := do(http.MethodPost, "/api/login/totp", map[string]string{"mfa_token": tok, "code": "123456"}, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("totp with no totp: %d", w.Code)
	}
	w = do(http.MethodPost, "/api/login/webauthn/begin", map[string]string{"mfa_token": tok}, "")
	if w.Code != http.StatusOK {
		t.Fatalf("login begin: %d %s", w.Code, w.Body.String())
	}
	reqOpts := decodeJSON(t, w)
	// Tampered signature fails and counts as a failure; the real one logs in.
	tampered := auth.assert(t, reqOpts)
	tampered["response"].(map[string]interface{})["signature"] = b64([]byte("nope"))
	if w := do(http.MethodPost, "/api/login/webauthn/finish", map[string]interface{}{"mfa_token": tok, "credential": tampered}, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("tampered assertion: %d %s", w.Code, w.Body.String())
	}
	w = do(http.MethodPost, "/api/login/webauthn/begin", map[string]string{"mfa_token": tok}, "")
	reqOpts = decodeJSON(t, w)
	w = do(http.MethodPost, "/api/login/webauthn/finish", map[string]interface{}{"mfa_token": tok, "credential": auth.assert(t, reqOpts)}, "")
	if w.Code != http.StatusOK || sessionCookie(w) == "" || decodeJSON(t, w)["user_id"] != "admin" {
		t.Fatalf("login finish: %d %s", w.Code, w.Body.String())
	}
	// The mfa token is consumed; sign count / last use recorded.
	if w := do(http.MethodPost, "/api/login/webauthn/begin", map[string]string{"mfa_token": tok}, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("token replay: %d", w.Code)
	}
	w = do(http.MethodGet, "/api/me/webauthn", nil, adminSess.ID)
	if !strings.Contains(w.Body.String(), `"last_used_at":"`) {
		t.Fatalf("last_used_at missing: %s", w.Body.String())
	}

	// A bearer API token cannot manage passkeys.
	_, plain, err := app.APITokens.Create(t.Context(), "admin", "t", "write", nil)
	if err != nil {
		t.Fatal(err)
	}
	req := jsonReq(t, http.MethodGet, "/api/me/webauthn", nil, "")
	req.Header.Set("Authorization", "Bearer "+plain)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("token on passkeys: %d", w.Code)
	}

	// Delete the passkey: login is single-step again. Then re-register and
	// let an admin reset clear it.
	w = do(http.MethodDelete, "/api/me/webauthn/"+b64(auth.credID), nil, adminSess.ID)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	w = do(http.MethodPost, "/api/login", map[string]string{"username": "admin", "password": "Admin123!"}, "")
	if sessionCookie(w) == "" {
		t.Fatalf("login after passkey removal: %s", w.Body.String())
	}
	w = do(http.MethodPost, "/api/me/webauthn/register/begin", nil, adminSess.ID)
	w = do(http.MethodPost, "/api/me/webauthn/register/finish", map[string]interface{}{"name": "again", "credential": auth.register(t, decodeJSON(t, w))}, adminSess.ID)
	if w.Code != http.StatusCreated {
		t.Fatalf("re-register: %d %s", w.Code, w.Body.String())
	}
	w = do(http.MethodDelete, "/api/users/admin/totp", nil, adminSess.ID)
	if w.Code != http.StatusNoContent {
		t.Fatalf("admin reset 2fa (passkeys only): %d %s", w.Code, w.Body.String())
	}
	if n, _ := app.WebAuthn.Count(t.Context(), "admin"); n != 0 {
		t.Fatalf("passkeys after admin reset = %d", n)
	}
}
