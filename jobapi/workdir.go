package jobapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/buildkite/agent/v4/internal/socket"
)

// setWorkdir records a working directory requested by a hook. The directory is
// applied by the executor after the hook process exits (see
// Server.TakePendingWorkdir).
//
// The endpoint accepts only absolute paths and does not stat the filesystem -
// the CLI is responsible for resolving relative paths (against the hook's actual
// working directory) and validating that the path exists before calling here.
func (s *Server) setWorkdir(w http.ResponseWriter, r *http.Request) {
	var req WorkdirSetRequest
	defer func() { _ = r.Body.Close() }()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err := socket.WriteError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	if req.Workdir == "" {
		if err := socket.WriteError(w, "workdir must not be empty", http.StatusUnprocessableEntity); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	if !filepath.IsAbs(req.Workdir) {
		if err := socket.WriteError(w, fmt.Sprintf("workdir must be an absolute path, got %q", req.Workdir), http.StatusUnprocessableEntity); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	s.mtx.Lock()
	s.pendingWorkdir = req.Workdir
	s.mtx.Unlock()

	resp := WorkdirSetResponse{Workdir: req.Workdir} //nolint:staticcheck // struct literal is clearer than conversion here
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.Logger.Errorf("Job API: couldn't encode or write response: %v", err)
	}
}
