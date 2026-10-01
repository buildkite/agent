package jobapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/internal/socket"
)

// listRedactions returns the values the job log redacts, so commands that store output elsewhere (like cache exec) can redact it too.
func (s *Server) listRedactions(w http.ResponseWriter, _ *http.Request) {
	s.mtx.RLock()
	redactions := s.redactors.Needles()
	s.mtx.RUnlock()

	if err := json.NewEncoder(w).Encode(&RedactionListResponse{Redactions: redactions}); err != nil {
		s.Logger.Errorf("Job API: couldn't write response: %v", err)
	}
}

func (s *Server) createRedaction(w http.ResponseWriter, r *http.Request) {
	payload := &RedactionCreateRequest{}
	if err := json.NewDecoder(r.Body).Decode(payload); err != nil {
		if err := socket.WriteError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	s.mtx.Lock()
	s.redactors.Add(payload.Redact, redact.GoEscaped(payload.Redact))
	s.mtx.Unlock()

	respBody := &RedactionCreateResponse{Redacted: payload.Redact}
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(respBody); err != nil {
		s.Logger.Errorf("Job API: couldn't write error: %v", err)
	}
}
