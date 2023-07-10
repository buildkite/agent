package agentapi

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultSocketPath constructs the default path for the Agent API socket.
func DefaultSocketPath(base string) string {
	return filepath.Join(base, fmt.Sprintf("agent-%d", os.Getpid()))
}

// LeaderPath returns the path to the socket pointing to the leader agent.
func LeaderPath(base string) string {
	return filepath.Join(base, "agent-leader")
}
