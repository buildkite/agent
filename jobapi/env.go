package jobapi

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/socket"
)

func (s *Server) getEnv(w http.ResponseWriter, _ *http.Request) {
	s.mtx.RLock()
	normalizedEnv := s.environ.Dump()
	s.mtx.RUnlock()

	resp := EnvGetResponse{Env: normalizedEnv}
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.Logger.Errorf("Job API: couldn't encode or write response: %v", err)
	}
}

func (s *Server) patchEnv(w http.ResponseWriter, r *http.Request) {
	var req EnvUpdateRequestPayload
	err := json.NewDecoder(r.Body).Decode(&req)
	defer r.Body.Close() //nolint:errcheck // HTTP request body close errors are inconsequential
	if err != nil {
		if err := socket.WriteError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	added := make([]string, 0, len(req.Env))
	updated := make([]string, 0, len(req.Env))
	protected := checkProtected(slices.Collect(maps.Keys(req.Env)))

	if len(protected) > 0 {
		err := socket.WriteError(
			w,
			fmt.Sprintf("the following environment variables are protected, and cannot be modified: % v", protected),
			http.StatusUnprocessableEntity,
		)
		if err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	nils := make([]string, 0, len(req.Env))

	for k, v := range req.Env {
		if v == nil {
			nils = append(nils, k)
		}
	}

	if len(nils) > 0 {
		err := socket.WriteError(
			w,
			fmt.Sprintf("removing environment variables (ie setting them to null) is not permitted on this endpoint. The following keys were set to null: % v", nils),
			http.StatusUnprocessableEntity,
		)
		if err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	s.mtx.Lock()
	for k, v := range req.Env {
		if _, ok := s.environ.Get(k); ok {
			updated = append(updated, k)
		} else {
			added = append(added, k)
		}
		s.environ.Set(k, *v)
	}
	s.mtx.Unlock()

	resp := EnvUpdateResponse{
		Added:   added,
		Updated: updated,
	}

	resp.Normalize()

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.Logger.Errorf("Job API: couldn't encode or write response: %v", err)
	}
}

func (s *Server) deleteEnv(w http.ResponseWriter, r *http.Request) {
	var req EnvDeleteRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	defer r.Body.Close() //nolint:errcheck // HTTP request body close errors are inconsequential
	if err != nil {
		err := socket.WriteError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest)
		if err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	protected := checkProtected(req.Keys)
	if len(protected) > 0 {
		err := socket.WriteError(
			w,
			fmt.Sprintf("the following environment variables are protected, and cannot be modified: % v", protected),
			http.StatusUnprocessableEntity,
		)
		if err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	s.mtx.Lock()
	deleted := make([]string, 0, len(req.Keys))
	for _, k := range req.Keys {
		if _, ok := s.environ.Get(k); ok {
			deleted = append(deleted, k)
			s.environ.Remove(k)
		}
	}
	s.mtx.Unlock()

	resp := EnvDeleteResponse{Deleted: deleted}
	resp.Normalize()

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.Logger.Errorf("Job API: couldn't encode or write response: %v", err)
	}
}

func checkProtected(candidates []string) []string {
	protected := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if _, ok := env.ProtectedEnv[c]; ok {
			protected = append(protected, c)
		}
	}
	return protected
}
