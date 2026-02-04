package socket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

// Server hosts a HTTP server on a Unix domain socket.
type Server struct {
	path    string
	svr     *http.Server
	started bool
}

// NewServer creates a server that, when started, will listen on a socket at the
// given path.
func NewServer(socketPath string, handler http.Handler) (*Server, error) {
	if len(socketPath) >= socketPathLength() {
		return nil, fmt.Errorf("socket path %s is too long (path length: %d, max %d characters). This is a limitation of your host OS", socketPath, len(socketPath), socketPathLength())
	}

	if err := os.MkdirAll(filepath.Dir(socketPath), os.FileMode(0o700)); err != nil {
		return nil, fmt.Errorf("creating socket directory: %w", err)
	}

	exists, err := socketExists(socketPath)
	if err != nil {
		return nil, err
	}

	if exists {
		return nil, fmt.Errorf("file already exists at socket path %s", socketPath)
	}

	return &Server{
		path: socketPath,
		svr:  &http.Server{Handler: handler},
	}, nil
}

// Start starts the server.
func (s *Server) Start() error {
	if s.started {
		return errors.New("server already started")
	}

	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return err
	}

	go s.svr.Serve(ln)
	s.started = true

	return nil
}

// Close immediately closes down the server. Prefer Shutdown for ordinary use.
func (s *Server) Close() error {
	if !s.started {
		return errors.New("server not started")
	}
	return s.svr.Close()
}

// Shutdown calls Shutdown on the inner HTTP server, which closes the socket.
// Shutdown performs a graceful shutdown, and is preferred over Close.
func (s *Server) Shutdown(ctx context.Context) error {
	if !s.started {
		return errors.New("server not started")
	}
	return s.svr.Shutdown(ctx)
}
