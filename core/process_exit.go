package core

import (
	"fmt"
	"strings"
)

// ProcessExit describes how a process exited: if it was signaled, what its
// exit code was
type ProcessExit struct {
	Status       int
	Signal       string
	SignalReason string
}

// Error allows ProcessExit to be passed through error returns.
func (e ProcessExit) Error() string {
	if e.Status == 0 {
		return "process exited normally"
	}
	bits := []string{fmt.Sprintf("status=%d", e.Status)}
	if e.Signal != "" {
		bits = append(bits, "signal="+e.Signal)
	}
	if e.SignalReason != "" {
		bits = append(bits, "reason="+e.SignalReason)
	}
	return "process exited with " + strings.Join(bits, ", ")
}
