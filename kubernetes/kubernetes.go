package kubernetes

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
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
	"github.com/buildkite/roko"
)

func init() {
	gob.Register(new(syscall.WaitStatus))
}

const defaultSocketPath = "/workspace/buildkite.sock"

func New(l logger.Logger, c Config) *Runner {
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

type Runner struct {
	logger   logger.Logger
	conf     Config
	mu       sync.Mutex
	listener net.Listener

	started, done, interrupt               chan struct{}
	startedOnce, closedOnce, interruptOnce sync.Once

	server  *rpc.Server
	mux     *http.ServeMux
	clients map[int]*clientResult
}

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

type Config struct {
	SocketPath     string
	ClientCount    int
	Stdout, Stderr io.Writer
	Env            []string
}

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

func (r *Runner) Started() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.started
}

func (r *Runner) Done() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.done
}

// Interrupts all clients, triggering graceful shutdown
func (r *Runner) Interrupt() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.interruptOnce.Do(func() {
		close(r.interrupt)
	})
	return nil
}

// Stops the RPC server, allowing Run to return immediately
func (r *Runner) Terminate() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closedOnce.Do(func() {
		close(r.done)
	})
	return nil
}

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

func (r *Runner) ClientStateUnknown() bool {
	for _, client := range r.clients {
		if client.State == stateUnknown {
			return true
		}
	}
	return false
}

// ==== sidecar api ====

type Empty struct{}
type Logs struct {
	Data []byte
}

type ExitCode struct {
	ID         int
	ExitStatus int
}

type Status struct {
	Ready bool
}

type RegisterResponse struct {
	Env []string
}

func (r *Runner) WriteLogs(args Logs, reply *Empty) error {
	r.startedOnce.Do(func() {
		close(r.started)
	})
	_, err := io.Copy(r.conf.Stdout, bytes.NewReader(args.Data))
	return err
}

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
		} else if client, found := r.clients[id-1]; found && client.State == stateExited {
			*reply = RunStateStart
		}
		return nil
	}
}

type Client struct {
	ID         int
	SocketPath string
	client     *rpc.Client
}

var errNotConnected = errors.New("client not connected")

func (c *Client) Connect(ctx context.Context) (*RegisterResponse, error) {
	if c.SocketPath == "" {
		c.SocketPath = defaultSocketPath
	}

	// Because k8s might run the containers "out of order", the server socket
	// might not exist yet. Try to connect several times.
	r := roko.NewRetrier(
		roko.WithMaxAttempts(30),
		roko.WithStrategy(roko.Constant(time.Second)),
	)
	client, err := roko.DoFunc(ctx, r, func(*roko.Retrier) (*rpc.Client, error) {
		return rpc.DialHTTP("unix", c.SocketPath)
	})
	if err != nil {
		return nil, err
	}
	c.client = client
	var resp RegisterResponse
	if err := c.client.Call("Runner.Register", c.ID, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Exit(exitStatus int) error {
	if c.client == nil {
		return errNotConnected
	}
	return c.client.Call("Runner.Exit", ExitCode{
		ID:         c.ID,
		ExitStatus: exitStatus,
	}, nil)
}

// Write implements io.Writer
func (c *Client) Write(p []byte) (int, error) {
	if c.client == nil {
		return 0, errNotConnected
	}
	n := len(p)
	err := c.client.Call("Runner.WriteLogs", Logs{
		Data: p,
	}, nil)
	return n, err
}

type WaitReadyResponse struct {
	Err error
	Status
}

type RunState int

const (
	RunStateWait RunState = iota
	RunStateStart
	RunStateInterrupt
)

var ErrInterrupt = errors.New("interrupt signal received")

func (c *Client) Await(ctx context.Context, desiredState RunState) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			var current RunState
			if err := c.client.Call("Runner.Status", c.ID, &current); err != nil {
				return err
			}
			if current == desiredState {
				return nil
			}
			if current == RunStateInterrupt {
				return ErrInterrupt
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
}

func (c *Client) Close() {
	c.client.Close()
}
