package jobapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/job/shell"
)

// Server is a Job API server. It provides an HTTP API with which to interact with the job currently running in the buildkite agent
// and allows jobs to introspect and mutate their own state
type Server struct {
	// SocketPath is the path to the socket that the server is (or will be) listening on
	SocketPath string
	Logger     shell.Logger

	environ *env.Environment
	token   string
	httpSvr *http.Server
	started bool
	mtx     sync.RWMutex
}

// NewServer creates a new Job API server
// socketPath is the path to the socket on which the server will listen
// environ is the environment which the server will mutate and inspect as part of its operation
func NewServer(logger shell.Logger, socketPath string, environ *env.Environment) (server *Server, token string, err error) {
	if len(socketPath) >= socketPathLength() {
		return nil, "", fmt.Errorf("socket path %s is too long (path length: %d, max %d characters). This is a limitation of your host OS", socketPath, len(socketPath), socketPathLength())
	}

	exists, err := socketExists(socketPath)
	if err != nil {
		return nil, "", err
	}

	if exists {
		return nil, "", fmt.Errorf("file already exists at socket path %s", socketPath)
	}

	token, err = generateToken(32)
	if err != nil {
		return nil, "", fmt.Errorf("generating token: %w", err)
	}

	return &Server{
		SocketPath: socketPath,
		Logger:     logger,
		environ:    environ,
		token:      token,
	}, token, nil
}

// Start starts the server in a goroutine, returning an error if the server can't be started
func (s *Server) Start() error {
	if s.started {
		return errors.New("server already started")
	}

	r := s.router()
	l, err := net.Listen("unix", s.SocketPath)
	if err != nil {
		return fmt.Errorf("listening on socket: %w", err)
	}

	s.httpSvr = &http.Server{Handler: r}
	go func() {
		_ = s.httpSvr.Serve(l)
	}()
	s.started = true

	s.Logger.Commentf("Job API server listening on %s", s.SocketPath)

	return nil
}

// Stop gracefully shuts the server down, blocking until all requests have been served or the grace period has expired
// It returns an error if the server has not been started
func (s *Server) Stop() error {
	if !s.started {
		return errors.New("server not started")
	}

	// Shutdown signal with grace period of 10 seconds
	shutdownCtx, serverStopCtx := context.WithTimeout(context.Background(), 10*time.Second)
	defer serverStopCtx()

	// Trigger graceful shutdown
	err := s.httpSvr.Shutdown(shutdownCtx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			s.Logger.Warningf("Job API server shutdown timed out, server shutdown forced")
		}
		return fmt.Errorf("shutting down Job API server: %w", err)
	}

	s.Logger.Commentf("Successfully shut down Job API server")

	return nil
}

// socketExists returns true if the socket path exists on linux and darwin
// on windows it always returns false, because of https://github.com/golang/go/issues/33357 (stat on sockets is broken on windows)
func socketExists(path string) (bool, error) {
	if runtime.GOOS == "windows" {
		return false, nil
	}

	_, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, fmt.Errorf("stat socket: %w", err)
	}

	return true, nil
}

func generateToken(len int) (string, error) {
	b := make([]byte, len)
	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("reading from random: %w", err)
	}

	withEqualses := base64.URLEncoding.EncodeToString(b)
	return strings.TrimRight(withEqualses, "="), nil // Trim the equals signs because they're not valid in env vars
}

func socketPathLength() int {
	switch runtime.GOOS {
	case "darwin", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris":
		return 104
	case "linux":
		fallthrough
	default:
		return 108
	}
}
