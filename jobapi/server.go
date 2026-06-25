package jobapi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/agent/v3/internal/socket"
)

// ServerOpts provides a way to configure a Server
type ServerOpts func(*Server)

func WithDebug() ServerOpts {
	return func(s *Server) {
		s.debug = true
	}
}

func WithNoCheckoutOverride() ServerOpts {
	return func(s *Server) {
		s.noCheckoutOverride = true
	}
}

// WithPromiseFailureDeclarer sets the function the /promise-failure endpoint
// uses to declare promised failures to the Buildkite API. Debouncing means it's
// called at most once per successfully-declared exit status. If unset (e.g. in
// tests), the endpoint returns an error.
func WithPromiseFailureDeclarer(d PromiseFailureDeclarer) ServerOpts {
	return func(s *Server) {
		s.promiseFailures.declare = d
	}
}

// PromiseFailureDeclarer declares a promised failure for the current job to the
// Buildkite API. It returns the status code of the most recent API response (0
// if none was received, e.g. a network error after exhausting retries) and an
// error describing any failure. A nil error means the declaration was accepted.
type PromiseFailureDeclarer func(ctx context.Context, exitStatus int, reason string) (statusCode int, err error)

// Server is a Job API server. It provides an HTTP API with which to interact with the job currently running in the buildkite agent
// and allows jobs to introspect and mutate their own state
type Server struct {
	// SocketPath is the path to the socket that the server is (or will be) listening on
	SocketPath         string
	Logger             shell.Logger
	debug              bool
	noCheckoutOverride bool

	mtx       sync.RWMutex
	environ   *env.Environment
	redactors *replacer.Mux

	// pendingWorkdir holds an absolute working directory requested by a hook via
	// the /workdir endpoint, waiting to be applied by the executor once the hook
	// process exits. Guarded by mtx.
	pendingWorkdir string

	// promiseFailures coalesces concurrent and repeated promise-failure calls.
	promiseFailures *promiseFailureCoordinator

	token   string
	sockSvr *socket.Server
}

// NewServer creates a new Job API server
// socketPath is the path to the socket on which the server will listen
// environ is the environment which the server will mutate and inspect as part of its operation
func NewServer(
	logger shell.Logger,
	socketPath string,
	environ *env.Environment,
	redactors *replacer.Mux,
	opts ...ServerOpts,
) (server *Server, token string, err error) {
	token, err = socket.GenerateToken(32)
	if err != nil {
		return nil, "", fmt.Errorf("generating token: %w", err)
	}

	s := &Server{
		SocketPath:      socketPath,
		Logger:          logger,
		environ:         environ,
		redactors:       redactors,
		promiseFailures: newPromiseFailureCoordinator(nil),
		token:           token,
	}

	for _, o := range opts {
		o(s)
	}

	svr, err := socket.NewServer(socketPath, s.router())
	if err != nil {
		return nil, "", fmt.Errorf("creating socket server: %w", err)
	}
	s.sockSvr = svr

	return s, token, err
}

// TakePendingWorkdir returns the working directory requested by a hook via the
// /workdir endpoint (if any) and clears the pending signal. The second return
// value reports whether a directory was pending. The applied directory persists
// in the executor's shell working directory; this only consumes the signal.
func (s *Server) TakePendingWorkdir() (string, bool) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.pendingWorkdir == "" {
		return "", false
	}
	wd := s.pendingWorkdir
	s.pendingWorkdir = ""
	return wd, true
}

// Start starts the server in a goroutine, returning an error if the server can't be started
func (s *Server) Start() error {
	if err := s.sockSvr.Start(); err != nil {
		return fmt.Errorf("starting socket server: %w", err)
	}

	if s.debug {
		s.Logger.Printf("~~~ Job API")
		s.Logger.Printf("Server listening on %s", s.SocketPath)
	}

	return nil
}

// Stop gracefully shuts the server down, blocking until all requests have been served or the grace period has expired
// It returns an error if the server has not been started
func (s *Server) Stop() error {
	// Shutdown signal with grace period of 10 seconds
	shutdownCtx, serverStopCtx := context.WithTimeout(context.Background(), 10*time.Second)
	defer serverStopCtx()

	// Trigger graceful shutdown
	err := s.sockSvr.Shutdown(shutdownCtx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			s.Logger.Warningf("Job API server shutdown timed out, server shutdown forced")
		}
		return fmt.Errorf("shutting down Job API server: %w", err)
	}

	if s.debug {
		s.Logger.Commentf("Successfully shut down Job API server")
	}

	return nil
}
