package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nullpo7z/vantyx/internal/httpapi"
)

const (
	defaultCertFile = "/app/certs/tls.crt"
	defaultKeyFile  = "/app/certs/tls.key"
	defaultRedirect = ":8080"
	defaultHTTPS    = ":8443"
)

func main() {
	certFile := defaultCertFile
	if v := os.Getenv("VANTYX_TLS_CERT_FILE"); v != "" {
		certFile = v
	}
	keyFile := defaultKeyFile
	if v := os.Getenv("VANTYX_TLS_KEY_FILE"); v != "" {
		keyFile = v
	}
	redirectAddr := defaultRedirect
	if v := os.Getenv("VANTYX_HTTP_REDIRECT_ADDR"); v != "" {
		redirectAddr = v
	}
	httpsAddr := defaultHTTPS
	if v := os.Getenv("VANTYX_HTTPS_ADDR"); v != "" {
		httpsAddr = v
	}

	tlsCert, err := loadOrGenerateCert(certFile, keyFile)
	if err != nil {
		log.Fatalf("TLS cert: %v", err)
	}

	app := httpapi.NewApp()
	httpsServer := &http.Server{
		Addr:         httpsAddr,
		Handler:      app.NewRouter(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{*tlsCert},
			MinVersion:   tls.VersionTLS12,
		},
	}

	redirectServer := &http.Server{
		Addr:         redirectAddr,
		Handler:      http.HandlerFunc(redirectToHTTPS),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		// #nosec G706 -- redirectAddr from env (VANTYX_HTTP_REDIRECT_ADDR)
		log.Printf("starting HTTP redirect server on %s", redirectAddr)
		if err := redirectServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("redirect server failed: %v", err)
		}
	}()
	go func() {
		// #nosec G706 -- httpsAddr from env (VANTYX_HTTPS_ADDR)
		log.Printf("starting HTTPS server on %s", httpsAddr)
		if err := httpsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Fatalf("https server failed: %v", err)
		}
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	<-stopCh

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := redirectServer.Shutdown(ctx); err != nil {
		log.Printf("redirect server shutdown: %v", err)
	}
	if err := httpsServer.Shutdown(ctx); err != nil {
		log.Printf("https server shutdown: %v", err)
	}
	log.Println("server shutdown completed")
}

// redirectToHTTPS always responds with 301 to the same path on HTTPS (cannot be disabled).
func redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if strings.HasSuffix(host, ":80") {
		host = host[:len(host)-3]
	} else if strings.HasSuffix(host, ":8080") {
		host = host[:len(host)-5]
	}
	u := "https://" + host + r.URL.RequestURI()
	http.Redirect(w, r, u, http.StatusMovedPermanently)
}

func loadOrGenerateCert(certFile, keyFile string) (*tls.Certificate, error) {
	// #nosec G703 -- certFile from env (VANTYX_TLS_CERT_FILE), not user input
	_, errCert := os.Stat(certFile)
	// #nosec G703 -- keyFile from env (VANTYX_TLS_KEY_FILE), not user input
	_, errKey := os.Stat(keyFile)
	if errCert == nil && errKey == nil {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, err
		}
		// #nosec G706 -- certFile from env (VANTYX_TLS_CERT_FILE)
		log.Printf("loaded TLS cert from %s", certFile)
		return &cert, nil
	}
	if errCert == nil || errKey == nil {
		return nil, os.ErrNotExist
	}
	log.Println("TLS cert/key not found; generating self-signed certificate")
	return generateSelfSigned(certFile, keyFile)
}

func generateSelfSigned(certFile, keyFile string) (*tls.Certificate, error) {
	dir := filepath.Dir(certFile)
	// #nosec G703 -- paths from env (VANTYX_TLS_*), not user input
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	dnsNames, ipAddrs := collectCertSANs()

	template := x509.Certificate{
		SerialNumber:          bigInt(1),
		Subject:               pkix.Name{CommonName: "vantyx"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddrs,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}

	// #nosec G304,G703 -- certFile from env (VANTYX_TLS_CERT_FILE), not user input
	certOut, err := os.Create(certFile)
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

	// #nosec G304,G703 -- keyFile from env (VANTYX_TLS_KEY_FILE), not user input
	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		_ = keyOut.Close()
		return nil, err
	}
	if err := keyOut.Close(); err != nil {
		return nil, err
	}

	return loadOrGenerateCert(certFile, keyFile)
}

func bigInt(n int64) *big.Int { return big.NewInt(n) }

// collectCertSANs returns DNS/IP SAN entries for the self-signed cert.
//
//   - Always includes localhost and loopback.
//   - Adds all non-loopback interface IPs (useful when running the binary directly on a server).
//   - Allows explicit override via env VANTYX_TLS_SANS (comma-separated list of DNS names or IPs).
//     Example: VANTYX_TLS_SANS="192.168.1.10,server.local"
//
// Note: In Docker, interface IPs are usually container IPs; set VANTYX_TLS_SANS to the host IP or DNS name if needed.
func collectCertSANs() ([]string, []net.IP) {
	dnsSet := map[string]struct{}{"localhost": {}}
	ipSet := map[string]net.IP{
		net.IPv4(127, 0, 0, 1).String(): net.IPv4(127, 0, 0, 1),
		net.IPv6loopback.String():       net.IPv6loopback,
	}

	// Add interface IPs (best-effort).
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				var ip net.IP
				switch v := a.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip == nil {
					continue
				}
				ip = ip.To16()
				if ip == nil {
					continue
				}
				if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
					continue
				}
				ipSet[ip.String()] = ip
			}
		}
	}

	// Add explicit SANs.
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
