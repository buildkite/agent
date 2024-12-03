package core

// ProcessExit describes how a process exited: if it was signaled, what its
// exit code was
type ProcessExit struct {
	Status       int
	Signal       string
	SignalReason string
}
