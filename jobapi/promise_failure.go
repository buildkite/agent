package jobapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/buildkite/agent/v3/internal/socket"
)

// promiseFailure tracks one attempt to declare a given exit status to the
// Buildkite API. The first caller declares it; concurrent callers wait on done
// and share the result.
type promiseFailure struct {
	done   chan struct{}          // closed when the attempt completes
	result PromiseFailureResponse // declaration result
}

// promiseFailureCoordinator owns promise-failure coalescing and caching. Each
// entry is an in-flight declaration or a cached success or terminal failure;
// transient failures are removed so a later call can retry.
type promiseFailureCoordinator struct {
	mu      sync.Mutex
	entries map[int]*promiseFailure
	declare PromiseFailureDeclarer
}

func newPromiseFailureCoordinator(declare PromiseFailureDeclarer) *promiseFailureCoordinator {
	return &promiseFailureCoordinator{
		entries: make(map[int]*promiseFailure),
		declare: declare,
	}
}

// terminalStatus reports whether an HTTP status is a definitive client error
// that won't change on retry (e.g. 409, 422), so the result can be cached. A 429
// is excluded because it's retryable.
func terminalStatus(status int) bool {
	return status >= 400 && status < 500 && status != http.StatusTooManyRequests
}

// Declare declares a promised failure through the configured declarer,
// coalescing repeated and concurrent calls for the same exit status. The first
// caller declares it (blocking on the API so it can return an accurate result),
// concurrent callers wait and share that outcome, and callers after a success or
// a terminal failure return from the cache. Transient failures aren't cached, so
// a later call can retry.
//
// Debouncing keys on exit status only, so for a given exit status the first
// caller's reason wins and later callers' reasons are ignored.
func (c *promiseFailureCoordinator) Declare(ctx context.Context, req PromiseFailureRequest) (result PromiseFailureResponse, err error) {
	if c == nil || c.declare == nil {
		return PromiseFailureResponse{}, errors.New("promise-failure coordinator is not configured")
	}

	c.mu.Lock()
	pf, found := c.entries[req.ExitStatus]
	if !found {
		pf = &promiseFailure{done: make(chan struct{})}
		c.entries[req.ExitStatus] = pf
	}
	c.mu.Unlock()

	if found {
		// A declaration is already in progress or done; wait for it and share
		// the outcome, unless this caller gives up first.
		select {
		case <-pf.done:
		case <-ctx.Done():
			return PromiseFailureResponse{}, ctx.Err()
		}

		result := pf.result
		result.Outcome = PromiseFailureDebounced
		return result, nil
	}

	// We're the first caller, so we declare while others wait on pf.done. Use a
	// background context so the declaration isn't abandoned if this caller
	// disconnects; the waiters depend on it.
	completed := false
	defer func() {
		if completed {
			return
		}
		// If the declarer panics, release waiters with an error and drop the entry
		// so a later call can retry.
		// Otherwise pf.done never closes and callers block forever.
		v := recover()
		pf.result = PromiseFailureResponse{
			Outcome:        PromiseFailureDeclared,
			Accepted:       false,
			UpstreamStatus: http.StatusInternalServerError,
			Error:          fmt.Sprintf("declaring promised failure panicked: %v", v),
			Terminal:       false,
		}
		c.mu.Lock()
		delete(c.entries, req.ExitStatus)
		close(pf.done)
		c.mu.Unlock()
		result = pf.result
		err = nil
	}()

	statusCode, err := c.declare(context.Background(), req.ExitStatus, req.Reason)
	pf.result = PromiseFailureResponse{
		Outcome:        PromiseFailureDeclared,
		Accepted:       err == nil,
		UpstreamStatus: statusCode,
	}
	if err != nil {
		pf.result.Error = err.Error()
		pf.result.Terminal = terminalStatus(statusCode)
	}
	completed = true

	c.mu.Lock()
	if !pf.result.Accepted && !pf.result.Terminal {
		// Evict transient failures (5xx, network, 429) so a later call can retry.
		// Terminal failures (other 4xx, e.g. 409/422) stay cached so repeated
		// calls don't keep hitting the Buildkite API. Waiters already hold pf and
		// still see this result either way.
		delete(c.entries, req.ExitStatus)
	}
	close(pf.done)
	c.mu.Unlock()

	return pf.result, nil
}

// handlePromiseFailure handles HTTP concerns for 'buildkite-agent job
// promise-failure'. The coordinator owns declaration, debouncing, and caching.
func (s *Server) handlePromiseFailure(w http.ResponseWriter, r *http.Request) {
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

	result, err := s.promiseFailures.Declare(r.Context(), *payload)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}

		if err := socket.WriteError(w, err, http.StatusInternalServerError); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	if err := json.NewEncoder(w).Encode(&result); err != nil {
		s.Logger.Errorf("Job API: couldn't encode or write response: %v", err)
	}
}
