package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// NewRouter constructs the main HTTP router for the Vantyx API.
func NewRouter() http.Handler {
	r := chi.NewRouter()

	// Health check
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return r
}
