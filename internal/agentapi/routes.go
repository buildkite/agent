package agentapi

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/buildkite/agent/v4/internal/socket"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// router defines all routes for the Agent API server.
func (s *Server) router(log *slog.Logger) chi.Router {
	r := chi.NewRouter()
	r.Use(
		// Agent API is quite chatty, so only log at Debug level.
		socket.LoggerMiddleware("Agent API", log.Debug),
		middleware.Recoverer,
		// All responses are in JSON.
		socket.HeadersMiddleware(http.Header{"Content-Type": []string{"application/json"}}),
	)

	r.Route("/api/leader/v0", func(r chi.Router) {
		r.Get("/ping", pingHandler(log))
		r.Route("/lock", s.lockSvr.routes)
	})

	return r
}

func pingHandler(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := &PingResponse{Now: time.Now()}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Error(fmt.Sprintf("Agent API: couldn't encode response body: %v", err))
		}
	}
}
