package jobapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/buildkite/agent/v4/internal/socket"
)

// maxRedactBody bounds a POST /redact request; cache exec sends at most 10 MiB of output, base64-encoded.
const maxRedactBody = 32 << 20

// redact returns output with the job log's secrets redacted, so commands that store output elsewhere (like
// cache exec) can redact it without the secrets leaving the job executor.
func (s *Server) redact(w http.ResponseWriter, r *http.Request) {
	payload := &RedactRequest{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRedactBody)).Decode(payload); err != nil {
		if err := socket.WriteError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	var out bytes.Buffer
	rd := s.redactors.Snapshot(&out)
	_, _ = rd.Write(payload.Output) // writes to a bytes.Buffer don't fail
	_ = rd.Flush()

	if err := json.NewEncoder(w).Encode(&RedactResponse{Redacted: out.Bytes()}); err != nil {
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

	s.redactors.Add(payload.Redact)

	respBody := &RedactionCreateResponse{Redacted: payload.Redact}
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(respBody); err != nil {
		s.Logger.Errorf("Job API: couldn't write error: %v", err)
	}
}
