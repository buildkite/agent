package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/logger"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// DefaultServerPath constructs the default path for the agent API socket.
func DefaultServerPath(base string) string {
	return filepath.Join(base, fmt.Sprintf("agent-%d.sock", os.Getpid()))
}

// LeaderPath returns the path to the socket pointing to the leader agent.
func LeaderPath(base string) string {
	return filepath.Join(base, "agent-leader.sock")
}

// Server hosts the Unix domain socket used for implementing the Agent API.
type Server struct {
	Logger logger.Logger

	mu    sync.Mutex
	locks map[string]string
	svr   *http.Server
}

// NewServer listens on a socket at the given path.
func NewServer(path string, logger logger.Logger) (*Server, error) {
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	svr := &http.Server{}
	s := &Server{
		Logger: logger,
		locks:  make(map[string]string),
		svr:    svr,
	}
	svr.Handler = s.router()
	go svr.Serve(ln)
	return s, nil
}

// Shutdown calls Shutdown on the inner HTTP server, which closes the socket.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.svr.Shutdown(ctx)
}

func (s *Server) router() chi.Router {
	r := chi.NewRouter()
	r.Use(
		loggerMiddleware(s.Logger),
		middleware.Recoverer,
		headersMiddleware,
	)

	r.Route("/api/leader/v0/lock", func(r chi.Router) {
		r.Get("/{key}", s.getLock)
		r.Patch("/{key}", s.patchLock)
	})

	return r
}

// getLock atomically retrieves the current lock value.
func (s *Server) getLock(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if key == "" {
		http.Error(w, "key missing", http.StatusNotFound)
		return
	}
	resp := &ValueResponse{
		Value: s.lockLoad(key),
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.Logger.Error("Leader API: couldn't encode response body: %v", err)
	}
}

// patchLock tries to atomically update the lock value.
func (s *Server) patchLock(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if key == "" {
		http.Error(w, "key missing", http.StatusNotFound)
		return
	}

	var req LockCASRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("couldn't decode request body: %v", err), http.StatusBadRequest)
	}

	v, ok := s.lockCAS(key, req.Old, req.New)
	resp := &LockCASResponse{
		Value:   v,
		Swapped: ok,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.Logger.Error("Leader API: couldn't encode response body: %v", err)
	}
}

// lockLoad atomically retrieves the current value for the lock.
func (s *Server) lockLoad(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.locks[key]
}

// lockCAS atomically attempts to swap the old value for the key for a new
// value. It reports whether the swap succeeded, returning the (new or existing)
// value.
func (s *Server) lockCAS(key, old, new string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks[key] == old {
		s.locks[key] = new
		return new, true
	}
	return s.locks[key], false
}

func loggerMiddleware(l logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t := time.Now()
			defer l.Info("Leader API:\t%s\t%s\t%s", r.Method, r.URL.Path, time.Since(t))
			next.ServeHTTP(w, r)
		})
	}
}

// HeadersMiddleware is a middleware that sets the common headers for all
// responses. At the moment, this is just Content-Type: application/json.
func headersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer next.ServeHTTP(w, r)

		w.Header().Set("Content-Type", "application/json")
	})
}
