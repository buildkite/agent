package jobapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/agent/v4/internal/socket"
)

// MaxRedactBody bounds a POST /redact request; cache exec sends at most its recorded-output limit, base64-encoded.
const MaxRedactBody = 32 << 20

// redact returns output chunks with the job log's secrets redacted, so commands that store output elsewhere (like cache exec) can redact it without the secrets leaving the job executor.
func (s *Server) redact(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRedactBody)
	payload := &RedactRequest{}
	if err := json.NewDecoder(r.Body).Decode(payload); err != nil {
		if err := socket.WriteError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	s.mtx.RLock()
	needles := s.redactors.Needles()
	s.mtx.RUnlock()

	chunks, err := RedactChunks(payload.Chunks, needles, redact.TokenPrefixes()...)
	if err != nil {
		if err := socket.WriteError(w, err, http.StatusInternalServerError); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}
	if err := json.NewEncoder(w).Encode(&RedactResponse{Chunks: chunks}); err != nil {
		s.Logger.Errorf("Job API: couldn't write response: %v", err)
	}
}

// RedactChunks redacts needles and prefixes from output chunks per stream, catching secrets split across chunks; the result keeps each chunk's stream and their rough order.
func RedactChunks(chunks []OutputChunk, needles []string, prefixes ...replacer.Prefix) ([]OutputChunk, error) {
	var out []OutputChunk
	newRedactor := func(stderr bool) *replacer.Replacer {
		r := redact.New(ChunkWriter{Chunks: &out, Stderr: stderr}, needles)
		r.AddPrefixes(prefixes...)
		return r
	}
	stdout, stderr := newRedactor(false), newRedactor(true)
	for _, c := range chunks {
		r := stdout
		if c.Stderr {
			r = stderr
		}
		if _, err := r.Write(c.Data); err != nil {
			return nil, err
		}
	}
	if err := errors.Join(stdout.Flush(), stderr.Flush()); err != nil {
		return nil, err
	}
	return out, nil
}

// ChunkWriter appends each write to Chunks as an OutputChunk on one stream.
type ChunkWriter struct {
	Chunks *[]OutputChunk
	Stderr bool
}

func (w ChunkWriter) Write(p []byte) (int, error) {
	*w.Chunks = append(*w.Chunks, OutputChunk{Stderr: w.Stderr, Data: bytes.Clone(p)})
	return len(p), nil
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
