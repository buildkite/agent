package jobapi

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

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
	defer func() { _ = r.Body.Close() }()
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		if err := socket.WriteError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	added := make([]string, 0, len(req.Env))
	updated := make([]string, 0, len(req.Env))
	protected := s.checkProtected(slices.Collect(maps.Keys(req.Env)))

	if len(protected) > 0 {
		err := socket.WriteError(
			w,
			s.protectedEnvMessage(protected),
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
	defer func() { _ = r.Body.Close() }()
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		err := socket.WriteError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest)
		if err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	protected := s.checkProtected(req.Keys)
	if len(protected) > 0 {
		err := socket.WriteError(
			w,
			s.protectedEnvMessage(protected),
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

func (s *Server) checkProtected(candidates []string) []string {
	protected := make([]string, 0, len(candidates))
	for _, c := range candidates {
		// The Job API is only accessible from within the job, so allow writes
		// to vars that allow write from within job.
		if env.IsProtectedFromWithinJob(c) || env.IsCheckoutLocked(c, s.checkoutOverrideMode) {
			protected = append(protected, c)
		}
	}
	return protected
}

// protectedEnvMessage builds the rejection message for protected candidates,
// noting when the rejection is due to the checkout-override lock rather than an
// always-protected var.
func (s *Server) protectedEnvMessage(protected []string) string {
	var msg strings.Builder
	fmt.Fprintf(&msg, "the following environment variables are protected, and cannot be modified: % v", protected)
	for _, p := range protected {
		if env.IsCheckoutLocked(p, s.checkoutOverrideMode) {
			fmt.Fprintf(&msg, ". Checkout-related variables are locked because BUILDKITE_CHECKOUT_OVERRIDE_MODE=%s", s.checkoutOverrideMode)
			break
		}
	}
	return msg.String()
}
