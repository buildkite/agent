package jobapi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/job/shell"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/agent/v3/internal/socket"
)

// ServerOption provides a way to configure a Server
type ServerOption interface {
	apply(*Server)
}

type loggerOpt struct {
	logger shell.Logger
	debug  bool
}

func (o *loggerOpt) apply(s *Server) {
	s.Logger = o.logger
	s.debug = o.debug
}

// WithLogger sets the logger for the server
func WithLogger(logger shell.Logger, debug bool) *loggerOpt {
	return &loggerOpt{logger: logger, debug: debug}
}

type socketPathOpt string

func (o socketPathOpt) apply(s *Server) {
	s.SocketPath = string(o)
}

// WithSocketPath sets the socket path for the server
func WithSocketPath(socketPath string) socketPathOpt {
	return socketPathOpt(socketPath)
}

type environOpt struct {
	env *env.Environment
}

func (o environOpt) apply(s *Server) {
	s.env = o.env
}

// WithEnviron sets the environment for the server
func WithEnvironment(e *env.Environment) environOpt {
	return environOpt{env: e}
}

type redactorsOpt struct {
	redactors *replacer.Mux
}

func (o redactorsOpt) apply(s *Server) {
	s.redactors = o.redactors
}

// WithRedactors sets the redactors for the server
func WithRedactors(r *replacer.Mux) redactorsOpt {
	return redactorsOpt{redactors: r}
}

type tokenOpt string

func (o tokenOpt) apply(s *Server) {
	s.token = string(o)
}

// WithToken sets the token for the server. If not set, a random token will be generated
func WithToken(token string) tokenOpt {
	return tokenOpt(token)
}

// Server is a Job API server. It provides an HTTP API with which to interact with the job currently
// running in the buildkite agent and allows jobs to introspect and mutate their own state
type Server struct {
	// SocketPath is the path to the socket that the server is (or will be) listening on
	SocketPath string
	Logger     shell.Logger
	debug      bool

	mtx       sync.RWMutex
	env       *env.Environment
	redactors *replacer.Mux

	token   string
	sockSvr *socket.Server
}

// NewServer creates a new Job API server
// socketPath is the path to the socket on which the server will listen
// environ is the environment which the server will mutate and inspect as part of its operation
func NewServer(opts ...ServerOption) (server *Server, token string, err error) {
	token, err = socket.GenerateToken(32)
	if err != nil {
		return nil, "", fmt.Errorf("generating token: %w", err)
	}

	s := &Server{token: token}

	for _, o := range opts {
		o.apply(s)
	}

	if s.Logger == nil {
		return nil, "", errors.New("logger is required")
	}
	if s.SocketPath == "" {
		return nil, "", errors.New("socket path is required")
	}
	if s.env == nil {
		s.env = env.New()
	}
	if s.redactors == nil {
		s.redactors = replacer.NewMux()
	}

	if s.sockSvr == nil {
		svr, err := socket.NewServer(s.SocketPath, s.router())
		if err != nil {
			return nil, "", fmt.Errorf("creating socket server: %w", err)
		}
		s.sockSvr = svr
	}

	return s, s.token, err
}

// Start starts the server in a goroutine, returning an error if the server can't be started
func (s *Server) Start() error {
	if err := s.sockSvr.Start(); err != nil {
		return fmt.Errorf("starting socket server: %w", err)
	}

	s.Logger.Printf("~~~ Job API")
	s.Logger.Printf("Server listening on %s", s.SocketPath)

	return nil
}

// Stop gracefully shuts the server down, blocking until all requests have been served or the grace
// period has expired. It returns an error if the server has not been started
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

	s.Logger.Commentf("Successfully shut down Job API server")

	return nil
}
