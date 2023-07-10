package agentapi

import (
	"github.com/buildkite/agent/v3/internal/socket"
	"github.com/buildkite/agent/v3/logger"
)

// Server hosts the Unix domain socket used for implementing the Agent API.
type Server struct {
	*socket.Server

	lockSvr *lockServer
}

// NewServer creates a new Agent API server that, when started, listens on the
// socketPath.
func NewServer(socketPath string, log logger.Logger) (*Server, error) {
	s := &Server{
		lockSvr: newLockServer(log),
	}
	svr, err := socket.NewServer(socketPath, s.router(log))
	if err != nil {
		return nil, err
	}
	s.Server = svr
	return s, nil
}
