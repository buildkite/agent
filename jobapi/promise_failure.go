package jobapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/buildkite/agent/v3/internal/socket"
)

// promiseFailure claims an exit status for a promised failure. The first caller
// to claim a given exit status receives claimed=true and is responsible for
// declaring the promised failure to the Buildkite API; subsequent callers
// receive claimed=false so that repeated calls don't hammer the API.
func (s *Server) promiseFailure(w http.ResponseWriter, r *http.Request) {
	payload := &PromiseFailureRequest{}
	if err := json.NewDecoder(r.Body).Decode(payload); err != nil {
		if err := socket.WriteError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	s.mtx.Lock()
	_, seen := s.promisedFailures[payload.ExitStatus]
	if !seen {
		s.promisedFailures[payload.ExitStatus] = struct{}{}
	}
	s.mtx.Unlock()

	respBody := &PromiseFailureResponse{Claimed: !seen}
	if err := json.NewEncoder(w).Encode(respBody); err != nil {
		s.Logger.Errorf("Job API: couldn't write response: %v", err)
	}
}
