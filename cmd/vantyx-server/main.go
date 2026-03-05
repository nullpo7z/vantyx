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
		log.Printf("starting HTTP redirect server on %s", redirectAddr)
		if err := redirectServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("redirect server failed: %v", err)
		}
	}()
	go func() {
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
	_, errCert := os.Stat(certFile)
	_, errKey := os.Stat(keyFile)
	if errCert == nil && errKey == nil {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, err
		}
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
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber:          bigInt(1),
		Subject:                pkix.Name{CommonName: "vantyx"},
		NotBefore:              time.Now(),
		NotAfter:               time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:               x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:            []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:               []string{"localhost"},
		IPAddresses:            []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}

	certOut, err := os.Create(certFile)
	if err != nil {
		return nil, err
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		certOut.Close()
		return nil, err
	}
	if err := certOut.Close(); err != nil {
		return nil, err
	}

	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		keyOut.Close()
		return nil, err
	}
	if err := keyOut.Close(); err != nil {
		return nil, err
	}

	return loadOrGenerateCert(certFile, keyFile)
}

func bigInt(n int64) *big.Int { return big.NewInt(n) }
