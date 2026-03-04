package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// newServer constructs the HTTP server used by Vantyx.
func newServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)

	return &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
}

func main() {
	addr := ":8080"
	if v := os.Getenv("VANTYX_HTTP_ADDR"); v != "" {
		addr = v
	}

	server := newServer(addr)

	go func() {
		log.Printf("starting vantyx server on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server failed: %v", err)
		}
	}()

	// graceful shutdown
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	<-stopCh

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("server shutdown failed: %v", err)
	}

	log.Println("server shutdown completed")
}

