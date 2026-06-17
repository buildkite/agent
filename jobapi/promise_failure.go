package jobapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/buildkite/agent/v3/internal/socket"
)

// promiseFailure tracks one attempt to declare a given exit status to the
// Buildkite API. The first caller declares it; concurrent callers wait on done
// and share the result.
type promiseFailure struct {
	done       chan struct{} // closed when the attempt completes
	statusCode int           // most recent Buildkite API status (0 if none received)
	err        error         // declaration result; nil means accepted
}

// terminalStatus reports whether an HTTP status is a definitive client error
// that won't change on retry (e.g. 409, 422), so the result can be cached. A 429
// is excluded because it's retryable.
func terminalStatus(status int) bool {
	return status >= 400 && status < 500 && status != http.StatusTooManyRequests
}

// promiseFailure declares a promised failure to the Buildkite API for
// 'buildkite-agent job promise-failure', debouncing repeated and concurrent
// calls so each exit status is declared at most once successfully. The first
// caller declares it (blocking on the API so it can return an accurate result),
// concurrent callers wait and share that outcome, and callers after a success or
// a terminal failure return from the cache. Transient failures aren't cached, so
// a later call can retry.
func (s *Server) promiseFailure(w http.ResponseWriter, r *http.Request) {
	payload := &PromiseFailureRequest{}
	if err := json.NewDecoder(r.Body).Decode(payload); err != nil {
		if err := socket.WriteError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	// A promised failure must be a positive exit status, so reject anything else
	// here rather than calling the Buildkite API.
	if payload.ExitStatus <= 0 {
		if err := socket.WriteError(w, fmt.Errorf("exit_status must be a positive integer"), http.StatusBadRequest); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	if s.declarePromiseFailure == nil {
		if err := socket.WriteError(w, fmt.Errorf("the Job API server has no promised-failure declarer configured"), http.StatusInternalServerError); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	s.mtx.Lock()
	pf, found := s.promiseFailures[payload.ExitStatus]
	if !found {
		pf = &promiseFailure{done: make(chan struct{})}
		s.promiseFailures[payload.ExitStatus] = pf
	}
	s.mtx.Unlock()

	if found {
		// A declaration is already in progress or done; wait for it and share
		// the outcome, unless this caller gives up first.
		select {
		case <-pf.done:
		case <-r.Context().Done():
			return
		}
	} else {
		// We're the first caller, so we declare while others wait on pf.done.
		// Use a background context so the declaration isn't abandoned if this
		// caller disconnects; the waiters depend on it.
		completed := false
		defer func() {
			if completed {
				return
			}
			// If the declarer panics, release waiters with an error and drop the
			// entry so a later call can retry, then re-panic for the recovery
			// middleware. Otherwise pf.done never closes and callers block forever.
			v := recover()
			pf.statusCode = http.StatusInternalServerError
			pf.err = fmt.Errorf("declaring promised failure panicked: %v", v)
			s.mtx.Lock()
			delete(s.promiseFailures, payload.ExitStatus)
			close(pf.done)
			s.mtx.Unlock()
			panic(v)
		}()

		pf.statusCode, pf.err = s.declarePromiseFailure(context.Background(), payload.ExitStatus, payload.Reason)
		completed = true

		s.mtx.Lock()
		if pf.err != nil && !terminalStatus(pf.statusCode) {
			// Evict transient failures (5xx, network, 429) so a later call can
			// retry. Terminal failures (other 4xx, e.g. 409/422) stay cached so
			// repeated calls don't keep hitting the Buildkite API. Waiters
			// already hold pf and still see this result either way.
			delete(s.promiseFailures, payload.ExitStatus)
		}
		close(pf.done)
		s.mtx.Unlock()
	}

	if pf.err != nil {
		status := pf.statusCode
		if status < 400 || status > 599 {
			// No usable error status (e.g. a network error, or an unexpected
			// non-error status); report a bad gateway.
			status = http.StatusBadGateway
		}
		if err := socket.WriteError(w, pf.err, status); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	// Success. A leading caller (found == false) declared it; any other caller
	// shared that result without calling the Buildkite API.
	outcome := PromiseFailureDeclared
	if found {
		outcome = PromiseFailureDebounced
	}
	if err := json.NewEncoder(w).Encode(&PromiseFailureResponse{Outcome: outcome}); err != nil {
		s.Logger.Errorf("Job API: couldn't encode or write response: %v", err)
	}
}
