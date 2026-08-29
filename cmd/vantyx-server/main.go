package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	// Embeds the IANA timezone database in the binary so time.LoadLocation
	// (used to validate users' timezone preferences) works even when the
	// container image has no system tzdata package installed.
	_ "time/tzdata"

	"github.com/nullpo7z/vantyx/internal/httpapi"
	"github.com/nullpo7z/vantyx/internal/sshd"
	"github.com/nullpo7z/vantyx/internal/tftp"
)

const (
	defaultCertFile           = "/app/certs/tls.crt"
	defaultKeyFile            = "/app/certs/tls.key"
	defaultRedirect           = ":8080"
	defaultHTTPS              = ":8443"
	defaultShutdownTimeoutSec = 10
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	certFile := filepath.Clean(defaultCertFile)
	if v := os.Getenv("VANTYX_TLS_CERT_FILE"); v != "" {
		certFile = filepath.Clean(v)
	}
	keyFile := filepath.Clean(defaultKeyFile)
	if v := os.Getenv("VANTYX_TLS_KEY_FILE"); v != "" {
		keyFile = filepath.Clean(v)
	}
	absCert, err := filepath.Abs(certFile)
	if err != nil {
		// #nosec G706 -- certFile from env, not user input
		slog.Error("TLS cert path", "path", certFile, "error", err)
		os.Exit(1)
	}
	certFile = absCert
	absKey, err := filepath.Abs(keyFile)
	if err != nil {
		// #nosec G706 -- keyFile from env, not user input
		slog.Error("TLS key path", "path", keyFile, "error", err)
		os.Exit(1)
	}
	keyFile = absKey
	redirectAddr := defaultRedirect
	if v := os.Getenv("VANTYX_HTTP_REDIRECT_ADDR"); v != "" {
		redirectAddr = v
	}
	httpsAddr := defaultHTTPS
	if v := os.Getenv("VANTYX_HTTPS_ADDR"); v != "" {
		httpsAddr = v
	}
	readTimeout := 15 * time.Second
	if v := os.Getenv("VANTYX_HTTPS_READ_TIMEOUT_SEC"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			// #nosec G706 -- v from env, not user input
			slog.Error("VANTYX_HTTPS_READ_TIMEOUT_SEC parse failed, using default 15s", "value", v, "error", err)
		} else {
			readTimeout = time.Duration(n) * time.Second
		}
	}

	if os.Getenv("VANTYX_EXTERNAL_HOST") == "" && strings.TrimSpace(os.Getenv("VANTYX_ALLOWED_HOSTS")) == "" {
		slog.Error("redirect requires VANTYX_EXTERNAL_HOST or VANTYX_ALLOWED_HOSTS for safe redirect target; set at least one to avoid redirecting to localhost")
		os.Exit(1)
	}

	tlsCert, err := loadOrGenerateCert(certFile, keyFile)
	if err != nil {
		slog.Error("TLS cert", "error", err, "hint", "if using self-signed cert, ensure the cert directory exists and is writable (e.g. pre-create /app/certs with correct ownership in non-root containers)")
		os.Exit(1)
	}

	app := httpapi.NewApp()
	ctx := context.Background()
	tftp.DisableAtStartup(ctx, app.TargetStore)
	tftp.StartServerIfNeeded(ctx, app.TargetStore)
	httpsHandler := corsMiddleware(app.NewRouter())
	// ReadTimeout covers the whole request including body; increase via VANTYX_HTTPS_READ_TIMEOUT_SEC for large uploads, or use TimeoutHandler/MaxBytesReader in router.
	httpsServer := &http.Server{
		Addr:              httpsAddr,
		Handler:           httpsHandler,
		ReadTimeout:       readTimeout,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{*tlsCert},
			MinVersion:   tls.VersionTLS13,
		},
	}

	redirectServer := &http.Server{
		Addr:              redirectAddr,
		Handler:           redirectToHTTPSHandler(),
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	var sshServer *sshd.Server
	if sshListen := strings.TrimSpace(os.Getenv("VANTYX_SSH_LISTEN")); sshListen != "" {
		recordingDir := strings.TrimSpace(os.Getenv("VANTYX_RECORDINGS_DIR"))
		var recordingStore sshd.RecordingStore
		if recordingDir != "" && app.DB != nil {
			recordingStore = app
		}
		var err error
		sshServer, err = sshd.NewServer(sshd.Config{
			UserStore:       app.UserStore,
			TargetStore:     app.TargetStore,
			GroupStore:      app.AccessGroupStore,
			SessionManager:  app.TerminalSessionManager,
			RecordingsDir:   recordingDir,
			RecordingStore:  recordingStore,
			SharingRegistry: app.SharingRegistry,
			SharingStore:    app.SharingStore,
			SharingBridges:  app.SharingBridges,
		})
		if err != nil {
			slog.Error("sshd setup failed", "error", err)
			os.Exit(1)
		}
		go func() {
			if err := sshServer.ListenAndServe(sshListen); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
				slog.Error("sshd failed", "error", err)
				// Cannot send to serverErrCh without making it size 3; use a separate channel or ignore after shutdown
			}
		}()
	}

	serverErrCh := make(chan error, 2)
	go func() {
		// #nosec G706 -- redirectAddr from env (VANTYX_HTTP_REDIRECT_ADDR)
		slog.Info("starting HTTP redirect server", "addr", redirectAddr)
		if err := redirectServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
		}
	}()
	go func() {
		// #nosec G706 -- httpsAddr from env (VANTYX_HTTPS_ADDR)
		slog.Info("starting HTTPS server", "addr", httpsAddr)
		if err := httpsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
		}
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	var hasServerError bool
	select {
	case sig := <-stopCh:
		slog.Info("received signal", "signal", sig)
	case err := <-serverErrCh:
		slog.Error("server failed", "error", err)
		hasServerError = true
	}

	shutdownSec := defaultShutdownTimeoutSec
	if v := os.Getenv("VANTYX_SHUTDOWN_TIMEOUT_SEC"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			// #nosec G706 -- v from env, not user input
			slog.Error("VANTYX_SHUTDOWN_TIMEOUT_SEC parse failed, using default", "value", v, "error", err)
		} else {
			shutdownSec = n
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(shutdownSec)*time.Second)
	defer cancel()

	// Shutdown HTTP servers and SSH server in parallel.
	var wg sync.WaitGroup
	shutdownCount := 2
	if sshServer != nil {
		shutdownCount++
	}
	errCh := make(chan error, shutdownCount)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errCh <- redirectServer.Shutdown(ctx)
	}()
	go func() {
		defer wg.Done()
		errCh <- httpsServer.Shutdown(ctx)
	}()
	if sshServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- sshServer.Shutdown()
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			slog.Error("server shutdown", "error", err)
		}
	}
	slog.Info("server shutdown completed")
	if hasServerError {
		os.Exit(1)
	}
}

// corsMiddleware adds CORS headers when VANTYX_CORS_ALLOWED_ORIGINS is set (ASVS V14.4.2). Comma-separated list, e.g. https://app.example.com.
func corsMiddleware(next http.Handler) http.Handler {
	origins := make(map[string]struct{})
	if v := strings.TrimSpace(os.Getenv("VANTYX_CORS_ALLOWED_ORIGINS")); v != "" {
		for _, o := range strings.Split(v, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				origins[o] = struct{}{}
			}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(origins) > 0 {
			w.Header().Add("Vary", "Origin")
		}
		origin := r.Header.Get("Origin")
		if _, ok := origins[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// redirectToHTTPSHandler returns a handler that 301-redirects to HTTPS. When VANTYX_EXTERNAL_HOST is set it is used (ASVS V5.1.4). When unset, r.Host is allowed only if it is in VANTYX_ALLOWED_HOSTS (comma-separated); otherwise 400 Bad Request or safe default localhost.
func redirectToHTTPSHandler() http.HandlerFunc {
	allowedHosts := make(map[string]struct{})
	if v := strings.TrimSpace(os.Getenv("VANTYX_ALLOWED_HOSTS")); v != "" {
		for _, h := range strings.Split(v, ",") {
			h = strings.TrimSpace(strings.ToLower(h))
			if h != "" {
				allowedHosts[h] = struct{}{}
			}
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		targetHost := os.Getenv("VANTYX_EXTERNAL_HOST")
		if targetHost == "" {
			requestHost := r.Host
			if h, _, err := net.SplitHostPort(r.Host); err == nil && h != "" {
				requestHost = h
			}
			requestHost = strings.TrimSpace(strings.ToLower(requestHost))
			if requestHost == "" {
				requestHost = "localhost"
			}
			if len(allowedHosts) > 0 {
				if _, ok := allowedHosts[requestHost]; !ok {
					// #nosec G706 -- requestHost logged for security audit (ASVS V7.1.1)
					slog.Warn("rejected invalid host header", "host", requestHost)
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte("Host not allowed"))
					return
				}
			} else {
				requestHost = "localhost"
			}
			addr := os.Getenv("VANTYX_HTTPS_ADDR")
			if addr == "" {
				addr = defaultHTTPS
			}
			port := "8443"
			if _, p, err := net.SplitHostPort(addr); err == nil && p != "" {
				port = p
			}
			targetHost = net.JoinHostPort(requestHost, port)
		}
		u := &url.URL{
			Scheme:   "https",
			Host:     targetHost,
			Path:     r.URL.Path,
			RawQuery: r.URL.RawQuery,
		}
		// Do not set HSTS on HTTP responses (RFC 6797 §7.2: MUST NOT on non-secure transport).
		http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
	}
}

func loadOrGenerateCert(certFile, keyFile string) (*tls.Certificate, error) {
	// Paths are normalized with filepath.Clean in main (ASVS V5.2.1).
	_, errCert := os.Stat(certFile)
	// #nosec G703 -- keyFile from env (VANTYX_TLS_KEY_FILE), not user input
	_, errKey := os.Stat(keyFile)
	if errCert == nil && errKey == nil {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, err
		}
		// #nosec G706 -- certFile from env (VANTYX_TLS_CERT_FILE)
		slog.Info("loaded TLS cert", "path", certFile)
		return &cert, nil
	}
	if errCert == nil || errKey == nil {
		return nil, os.ErrNotExist
	}
	slog.Info("TLS cert/key not found; generating self-signed certificate")
	return generateSelfSigned(certFile, keyFile)
}

func generateSelfSigned(certFile, keyFile string) (*tls.Certificate, error) {
	dir := filepath.Dir(certFile)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("create cert dir %s: %w (pre-create directory with correct ownership when running as non-root)", dir, err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	dnsNames, ipAddrs := collectCertSANs()

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: "vantyx"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddrs,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}

	// #nosec G304,G302 -- certFile from env. 0644 for cert (public); key uses 0600 (ASVS V14.1.1).
	certOut, err := os.OpenFile(certFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		_ = certOut.Close()
		return nil, err
	}
	if err := certOut.Close(); err != nil {
		return nil, err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- keyFile cleaned in main (VANTYX_TLS_KEY_FILE)
	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		_ = keyOut.Close()
		return nil, err
	}
	if err := keyOut.Close(); err != nil {
		return nil, err
	}

	// Load the written files so we return a tls.Certificate without recursing into loadOrGenerateCert.
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	// #nosec G706 -- certFile from env (VANTYX_TLS_CERT_FILE)
	slog.Info("generated and loaded self-signed TLS cert", "path", certFile)
	return &cert, nil
}

// collectCertSANs returns DNS/IP SAN entries for the self-signed cert.
// Only localhost/loopback and entries from VANTYX_TLS_SANS are included (ASVS V14.1.2: no interface IPs to avoid leaking container network topology).
//
//   - Always includes localhost and loopback.
//   - VANTYX_TLS_SANS: comma-separated DNS names or IPs, e.g. VANTYX_TLS_SANS="192.168.1.10,server.local"
func collectCertSANs() ([]string, []net.IP) {
	dnsSet := map[string]struct{}{"localhost": {}}
	ipSet := map[string]net.IP{
		net.IPv4(127, 0, 0, 1).String(): net.IPv4(127, 0, 0, 1),
		net.IPv6loopback.String():       net.IPv6loopback,
	}

	// Add explicit SANs from env only.
	if v := strings.TrimSpace(os.Getenv("VANTYX_TLS_SANS")); v != "" {
		parts := strings.Split(v, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if ip := net.ParseIP(p); ip != nil {
				ipSet[ip.String()] = ip
				continue
			}
			// Reject obvious invalid entries early.
			if strings.ContainsAny(p, " \t\r\n") {
				continue
			}
			dnsSet[p] = struct{}{}
		}
	}

	dns := make([]string, 0, len(dnsSet))
	for n := range dnsSet {
		dns = append(dns, n)
	}
	ips := make([]net.IP, 0, len(ipSet))
	for _, ip := range ipSet {
		ips = append(ips, ip)
	}

	// Ensure we return something valid.
	if len(dns) == 0 && len(ips) == 0 {
		// Should not happen, but keep cert generation safe.
		dns = []string{"localhost"}
		ips = []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	}

	return dns, ips
}
