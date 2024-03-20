package agentapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/buildkite/agent/v3/internal/socket"
	"github.com/buildkite/agent/v3/logger"
	"github.com/go-chi/chi/v5"
)

// lockServer serves lock requests using a lockState.
type lockServer struct {
	logger logger.Logger
	locks  *lockState
}

// newLockServer creates a lockServer containing a new empty lockState.
func newLockServer(logger logger.Logger) *lockServer {
	return &lockServer{
		logger: logger,
		locks:  newLockState(),
	}
}

// routes defines routes for the lockServer.
func (s *lockServer) routes(r chi.Router) {
	r.Get("/", s.getLock)
	r.Patch("/", s.patchLock)
}

// getLock atomically retrieves the current lock value.
func (s *lockServer) getLock(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		if err := socket.WriteError(w, "key missing", http.StatusNotFound); err != nil {
			s.logger.Error("Agent API: couldn't write error: %v", err)
		}
		return
	}
	resp := &ValueResponse{
		Value: s.locks.load(key),
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("Agent API: couldn't encode response body: %v", err)
	}
}

// patchLock tries to atomically update the lock value.
func (s *lockServer) patchLock(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		if err := socket.WriteError(w, "key missing", http.StatusNotFound); err != nil {
			s.logger.Error("Agent API: couldn't write error: %v", err)
		}
		return
	}

	var req LockCASRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err := socket.WriteError(w, fmt.Sprintf("couldn't decode request body: %v", err), http.StatusBadRequest); err != nil {
			s.logger.Error("Agent API: couldn't write error: %v", err)
		}
		return
	}

	v, ok := s.locks.cas(key, req.Old, req.New)
	resp := &LockCASResponse{
		Value:   v,
		Swapped: ok,
	}
	// *** Attention security researchers ***
	// Before reporting a JSON injection here, please prepare the following
	// information to include in your report:
	// - an example input that the Go standard library encoding/json package
	//   fails to correctly escape, thus leading to injection in the output, and
	// - practical consequences of a successful JSON injection, given that this
	//   API is, by default, only accessible to the same user on the same host.
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("Agent API: couldn't encode response body: %v", err)
	}
}
