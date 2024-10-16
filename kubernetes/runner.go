package kubernetes

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"syscall"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/process"
)

func init() {
	gob.Register(new(syscall.WaitStatus))
}

const defaultSocketPath = "/workspace/buildkite.sock"

type RunnerConfig struct {
	SocketPath     string
	ClientCount    int
	Stdout, Stderr io.Writer
	Env            []string
}

// NewRunner returns a runner, implementing the agent's jobRunner interface.
func NewRunner(l logger.Logger, c RunnerConfig) *Runner {
	if c.SocketPath == "" {
		c.SocketPath = defaultSocketPath
	}
	clients := make(map[int]*clientResult, c.ClientCount)
	for i := range c.ClientCount {
		clients[i] = &clientResult{}
	}
	return &Runner{
		logger:    l,
		conf:      c,
		clients:   clients,
		server:    rpc.NewServer(),
		mux:       http.NewServeMux(),
		done:      make(chan struct{}),
		started:   make(chan struct{}),
		interrupt: make(chan struct{}),
	}
}

// Runner implements the agent's jobRunner interface, but instead of directly
// managing a subprocess, it runs a socket server that is connected to from
// another container.
type Runner struct {
	logger   logger.Logger
	conf     RunnerConfig
	mu       sync.Mutex
	listener net.Listener

	started, done, interrupt               chan struct{}
	startedOnce, closedOnce, interruptOnce sync.Once

	server  *rpc.Server
	mux     *http.ServeMux
	clients map[int]*clientResult
}

// Run runs the socket server.
func (r *Runner) Run(ctx context.Context) error {
	r.server.Register(r)
	r.mux.Handle(rpc.DefaultRPCPath, r.server)

	oldUmask, err := Umask(0) // set umask of socket file to 0777 (world read-write-executable)
	if err != nil {
		return fmt.Errorf("failed to set socket umask: %w", err)
	}
	l, err := (&net.ListenConfig{}).Listen(ctx, "unix", r.conf.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	defer l.Close()
	defer os.Remove(r.conf.SocketPath)

	Umask(oldUmask) // change back to regular umask
	r.listener = l
	go http.Serve(l, r.mux)

	<-r.done
	return nil
}

// Started returns a channel that is closed when the job has started running.
func (r *Runner) Started() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.started
}

// Done returns a channel that is closed when the job is completed.
func (r *Runner) Done() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.done
}

// Interrupts all clients, triggering graceful shutdown.
func (r *Runner) Interrupt() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.interruptOnce.Do(func() {
		close(r.interrupt)
	})
	return nil
}

// Terminate stops the RPC server, allowing Run to return immediately.
func (r *Runner) Terminate() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closedOnce.Do(func() {
		close(r.done)
	})
	return nil
}

// WaitStatus returns a wait status that represents all the clients.
func (r *Runner) WaitStatus() process.WaitStatus {
	ws := waitStatus{}
	for _, client := range r.clients {
		if client.ExitStatus != 0 {
			return waitStatus{Code: client.ExitStatus}
		}

		// use an unusual status code to distinguish this unusual state
		if client.State == stateUnknown {
			ws.Code -= 10
		}
	}
	return ws
}

// ClientStateUnknown reports whether anhy of the client states is stateUnknown.
func (r *Runner) ClientStateUnknown() bool {
	for _, client := range r.clients {
		if client.State == stateUnknown {
			return true
		}
	}
	return false
}

// ==== sidecar api ====

// Empty is an empty RPC message.
type Empty struct{}

// WriteLogs is called to pass logs on to Buildkite.
func (r *Runner) WriteLogs(args Logs, reply *Empty) error {
	r.startedOnce.Do(func() {
		close(r.started)
	})
	_, err := io.Copy(r.conf.Stdout, bytes.NewReader(args.Data))
	return err
}

// Logs is an RPC message that contains log data.
type Logs struct {
	Data []byte
}

// Exit is called when the client exits.
func (r *Runner) Exit(args ExitCode, reply *Empty) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	client, found := r.clients[args.ID]
	if !found {
		return fmt.Errorf("unrecognized client id: %d", args.ID)
	}
	r.logger.Info("client %d exited with code %d", args.ID, args.ExitStatus)
	client.ExitStatus = args.ExitStatus
	client.State = stateExited
	if client.ExitStatus != 0 {
		r.closedOnce.Do(func() {
			close(r.done)
		})
	}

	allExited := true
	for _, client := range r.clients {
		allExited = client.State == stateExited && allExited
	}
	if allExited {
		r.closedOnce.Do(func() {
			close(r.done)
		})
	}
	return nil
}

// ExitCode is an RPC message that specifies an exit status for a client ID.
type ExitCode struct {
	ID         int
	ExitStatus int
}

// Register is called when the client registers with the runner. The reply
// contains the env vars that would normally be in the environment of the
// bootstrap subcommand, particularly, the agent session token.
func (r *Runner) Register(id int, reply *RegisterResponse) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startedOnce.Do(func() {
		close(r.started)
	})
	client, found := r.clients[id]
	if !found {
		return fmt.Errorf("client id %d not found", id)
	}
	if client.State != stateUnknown {
		return fmt.Errorf("client id %d already registered", id)
	}
	r.logger.Info("client %d connected", id)
	client.State = stateConnected

	reply.Env = r.conf.Env
	return nil
}

// RegisterResponse is an RPC message to registering clients containing info
// needed to run.
type RegisterResponse struct {
	Env []string
}

// Status is called by the client to check the status of the job, so that it can
// pack things up if the job is cancelled.
func (r *Runner) Status(id int, reply *RunState) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	select {
	case <-r.done:
		return rpc.ErrShutdown

	case <-r.interrupt:
		*reply = RunStateInterrupt
		return nil

	default:
		if id == 0 {
			*reply = RunStateStart
			return nil
		}
		if client, found := r.clients[id-1]; found && client.State == stateExited {
			*reply = RunStateStart
		}
		return nil
	}
}

// RunState is an RPC message that describes to a client whether the job should
// continue waiting before running, start running, or stop running.
type RunState int

const (
	// RunStateWait means the job is not ready to start executing yet.
	RunStateWait RunState = iota

	// RunStateStart means the job can begin.
	RunStateStart

	// RunStateInterrupt means the job is cancelled or should be terminated for
	// some other reason.
	RunStateInterrupt
)

// ==== related types and consts ====

type clientResult struct {
	ExitStatus int
	State      clientState
}

type clientState int

const (
	stateUnknown clientState = iota
	stateConnected
	stateExited
)

type waitStatus struct {
	Code       int
	SignalCode *int
}

func (w waitStatus) ExitStatus() int {
	return w.Code
}

func (w waitStatus) Signal() syscall.Signal {
	var signal syscall.Signal
	return signal
}

func (w waitStatus) Signaled() bool {
	return false
}
