package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server is the Galacta HTTP API server.
type Server struct {
	router  *chi.Mux
	handler *Handler
	port    int
}

// NewServer creates a new API server.
func NewServer(h *Handler, port int) *Server {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)
	r.Use(jsonContentType)

	r.Get("/health", h.Health)
	r.Post("/sessions", h.CreateSession)
	r.Get("/sessions", h.ListSessions)
	r.Get("/sessions/{id}", h.GetSession)
	r.Delete("/sessions/{id}", h.DeleteSession)
	r.Post("/sessions/{id}/message", h.RunMessage)
	r.Post("/sessions/{id}/permission/{requestID}", h.RespondPermission)
	r.Post("/sessions/{id}/question/{requestID}", h.RespondQuestion)
	r.Get("/sessions/{id}/messages", h.ListMessages)

	return &Server{router: r, handler: h, port: port}
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf(":%d", s.port)
	return http.ListenAndServe(addr, s.router)
}

// apiResponse is the standard JSON envelope.
type apiResponse struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(&apiResponse{
		OK:   status >= 200 && status < 300,
		Data: data,
	})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(&apiResponse{
		OK:    false,
		Error: msg,
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func jsonContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't set for SSE endpoints
		if r.URL.Path != "" && r.Method == "POST" {
			// Handler will set its own content type for SSE
		}
		next.ServeHTTP(w, r)
	})
}
