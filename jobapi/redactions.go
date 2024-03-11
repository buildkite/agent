package jobapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/buildkite/agent/v3/internal/socket"
)

func (s *Server) createRedaction(w http.ResponseWriter, r *http.Request) {
	payload := &RedactionCreateRequest{}
	if err := json.NewDecoder(r.Body).Decode(payload); err != nil {
		if err := socket.WriteError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	s.mtx.Lock()
	s.redactors.Add(payload.Redact)
	s.mtx.Unlock()

	respBody := &RedactionCreateResponse{Redacted: payload.Redact}
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(respBody); err != nil {
		s.Logger.Errorf("Job API: couldn't write error: %v", err)
	}
}
