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
	"time"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/process"
)

func init() {
	gob.Register(new(syscall.WaitStatus))
}

const defaultSocketPath = "/workspace/buildkite.sock"

type RunnerConfig struct {
	SocketPath         string
	ClientCount        int
	Stdout, Stderr     io.Writer
	Env                []string
	ClientStartTimeout time.Duration
	ClientLostTimeout  time.Duration
}

// NewRunner returns a runner, implementing the agent's jobRunner interface.
func NewRunner(l logger.Logger, c RunnerConfig) *Runner {
	if c.SocketPath == "" {
		c.SocketPath = defaultSocketPath
	}
	clients := make([]*clientResult, c.ClientCount)
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

		// Buffered in case startupCheck is disabled
		startCounter: make(chan struct{}, c.ClientCount),
	}
}

// Runner implements the agent's jobRunner interface, but instead of directly
// managing a subprocess, it runs a socket server that is connected to from
// another container.
type Runner struct {
	logger   logger.Logger
	conf     RunnerConfig
	listener net.Listener

	// Channels that are closed at certain points in the job lifecycle
	started, done, interrupt chan struct{}

	// Guards the closing of the above channels to ensure they are only closed once
	startedOnce, doneOnce, interruptOnce sync.Once

	// Used to count clients as they connect
	startCounter chan struct{}

	server  *rpc.Server
	mux     *http.ServeMux
	clients []*clientResult
}

// Run runs the socket server.
func (r *Runner) Run(ctx context.Context) error {
	r.server.Register(r)
	r.mux.Handle(rpc.DefaultRPCPath, r.server)

	oldUmask, err := Umask(0) // set umask of socket file to 0o777 (world read-write-executable)
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

	if r.conf.ClientLostTimeout > 0 {
		go r.livenessCheck(ctx)
	}

	if err := r.startupCheck(ctx); err != nil {
		return err
	}

	<-r.done
	return nil
}

// startupCheck blocks until all containers have connected, or times out.
func (r *Runner) startupCheck(ctx context.Context) error {
	if r.conf.ClientStartTimeout <= 0 { // check is disabled
		return nil
	}

	// wait for a value on startCounter once per client
	timeout := time.After(r.conf.ClientStartTimeout)
	for range r.clients {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-timeout:
			return fmt.Errorf("timed out waiting %v for all containers to connect", r.conf.ClientStartTimeout)

		case <-r.done:
			return nil

		case <-r.startCounter:
			// Another client has started!
		}
	}
	return nil
}

func (r *Runner) livenessCheck(ctx context.Context) {
	// 100ms chosen for snappiness; we should easily be able to scan all
	// clients quickly.
	tick := time.NewTicker(100 * time.Millisecond)

	for {
		select {
		case <-ctx.Done():
			return

		case <-r.done:
			return

		case <-tick.C:
			// Scan through the clients to see if any have become lost.
			for id, client := range r.clients {
				client.mu.Lock()

				// If the client has connected, and was last heard from too
				// long ago, it's lost.
				// This usually happens if k8s has OOM-killed the container.
				// The next client in the sequence won't have started yet, so
				// we can just terminate.
				lhf := time.Since(client.LastHeardFrom)
				if client.State == StateConnected && lhf > r.conf.ClientLostTimeout {
					r.logger.Error("Container (ID %d) was last heard from %v ago; marking lost and self-terminating...", id, lhf)
					client.State = StateLost
					r.Terminate()
				}
				client.mu.Unlock()
			}

		}
	}
}

// Started returns a channel that is closed when the job has started running.
// (At least one client container has connected.)
func (r *Runner) Started() <-chan struct{} { return r.started }

func (r *Runner) markStarted() { r.startedOnce.Do(func() { close(r.started) }) }

// Done returns a channel that is closed when the job is completed.
func (r *Runner) Done() <-chan struct{} { return r.done }

// Interrupts all clients, triggering graceful shutdown.
func (r *Runner) Interrupt() error {
	r.interruptOnce.Do(func() { close(r.interrupt) })
	return nil
}

// Terminate allows Run to return immediately, halting the RPC server.
func (r *Runner) Terminate() error {
	r.doneOnce.Do(func() { close(r.done) })
	return nil
}

// WaitStatus returns a wait status that represents all the clients.
func (r *Runner) WaitStatus() process.WaitStatus {
	for _, client := range r.clients {
		client.mu.Lock()
		exitStatus, state := client.ExitStatus, client.State
		client.mu.Unlock()

		if exitStatus != 0 {
			return waitStatus{Code: exitStatus}
		}

		// use an unusual status code to distinguish unusual states
		switch state {
		case StateLost:
			return waitStatus{Code: -7}
		case StateNotYetConnected:
			return waitStatus{Code: -10}
		}
	}
	return waitStatus{}
}

// AnyClientIn reports whether any of the clients are in a particular state.
func (r *Runner) AnyClientIn(state ClientState) bool {
	for _, client := range r.clients {
		client.mu.Lock()
		s := client.State
		client.mu.Unlock()

		if s == state {
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
	r.markStarted()
	_, err := io.Copy(r.conf.Stdout, bytes.NewReader(args.Data))
	return err
}

// Logs is an RPC message that contains log data.
type Logs struct {
	Data []byte
}

// Exit is called when the client exits.
func (r *Runner) Exit(args ExitCode, reply *Empty) error {
	if args.ID < 0 || args.ID >= len(r.clients) {
		return fmt.Errorf("unrecognized client id: %d", args.ID)
	}
	client := r.clients[args.ID]
	r.logger.Info("client %d exited with code %d", args.ID, args.ExitStatus)

	client.mu.Lock()
	client.ExitStatus = args.ExitStatus
	client.State = StateExited
	client.mu.Unlock()

	if args.ExitStatus != 0 {
		r.Terminate()
	}

	allTerminal := true
	for _, client := range r.clients {
		client.mu.Lock()
		if client.State == StateNotYetConnected || client.State == StateConnected {
			allTerminal = false
		}
		client.mu.Unlock()
		if !allTerminal {
			break
		}
	}
	if allTerminal {
		r.Terminate()
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
	if id < 0 || id >= len(r.clients) {
		return fmt.Errorf("unrecognized client id: %d", id)
	}

	r.markStarted()
	r.startCounter <- struct{}{}

	client := r.clients[id]
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.State != StateNotYetConnected {
		return fmt.Errorf("client id %d already registered", id)
	}
	r.logger.Info("client %d connected", id)
	client.LastHeardFrom = time.Now()
	client.State = StateConnected

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
// If the client stops calling Status before calling Exit, we assume it is lost.
func (r *Runner) Status(id int, reply *RunState) error {
	if id < 0 || id >= len(r.clients) {
		return fmt.Errorf("unrecognized client id: %d", id)
	}

	client := r.clients[id]
	client.mu.Lock()
	client.LastHeardFrom = time.Now()
	client.mu.Unlock()

	select {
	case <-r.done:
		return rpc.ErrShutdown

	case <-r.interrupt:
		*reply = RunStateInterrupt
		return nil

	default:
		// First client should start first.
		if id == 0 {
			*reply = RunStateStart
			return nil
		}

		// Client N can start after Client N-1 has exited.
		client := r.clients[id-1]
		client.mu.Lock()
		defer client.mu.Unlock()
		if client.State == StateExited {
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
	mu            sync.Mutex
	ExitStatus    int
	State         ClientState
	LastHeardFrom time.Time
}

type ClientState int

const (
	StateNotYetConnected ClientState = iota
	StateConnected
	StateExited
	StateLost
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
