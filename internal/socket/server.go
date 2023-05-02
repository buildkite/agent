package socket

import (
	"context"
	"net"
	"net/http"
)

// Server hosts a HTTP server on a Unix domain socket.
type Server struct {
	svr *http.Server
}

// NewServer listens on a socket at the given path and starts the server.
func NewServer(path string, handler http.Handler) (*Server, error) {
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	svr := &http.Server{Handler: handler}
	go svr.Serve(ln)
	return &Server{svr: svr}, nil
}

// Close immediately closes down the server. Prefer Shutdown for ordinary use.
func (s *Server) Close() error {
	return s.svr.Close()
}

// Shutdown calls Shutdown on the inner HTTP server, which closes the socket.
// Shutdown performs a graceful shutdown, and is preferred over Close.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.svr.Shutdown(ctx)
}
