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
	for i := 0; i < c.ClientCount; i++ {
		clients[i] = &clientResult{}
	}
	return &Runner{
		logger:  l,
		conf:    c,
		clients: clients,
		server:  rpc.NewServer(),
		mux:     http.NewServeMux(),
		done:    make(chan struct{}),
		started: make(chan struct{}),
	}
}

type Runner struct {
	logger        logger.Logger
	conf          Config
	mu            sync.Mutex
	listener      net.Listener
	started, done chan struct{}
	startedOnce,
	closedOnce sync.Once
	server  *rpc.Server
	mux     *http.ServeMux
	clients map[int]*clientResult
}

type clientResult struct {
	ExitStatus process.WaitStatus
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
	AccessToken    string
}

func (r *Runner) Run(ctx context.Context) error {
	r.server.Register(r)
	r.mux.Handle(rpc.DefaultRPCPath, r.server)

	l, err := (&net.ListenConfig{}).Listen(ctx, "unix", r.conf.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	defer l.Close()
	defer os.Remove(r.conf.SocketPath)
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

func (r *Runner) Interrupt() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	panic("unimplemented")
}

func (r *Runner) Terminate() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	panic("unimplemented")
}

func (r *Runner) WaitStatus() process.WaitStatus {
	var ws process.WaitStatus
	for _, client := range r.clients {
		if client.ExitStatus.ExitStatus() != 0 {
			return client.ExitStatus
		}
		if client.ExitStatus.Signaled() {
			return client.ExitStatus
		}
		// just return any ExitStatus if we don't find any "interesting" ones
		ws = client.ExitStatus
	}
	return ws
}

// ==== sidecar api ====

type Empty struct{}
type Logs struct {
	Data []byte
}

type ExitCode struct {
	ID         int
	ExitStatus process.WaitStatus
}

type Status struct {
	Ready       bool
	AccessToken string
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
	r.logger.Info("client %d exited with code %d", args.ID, args.ExitStatus.ExitStatus())
	client.ExitStatus = args.ExitStatus
	client.State = stateExited
	if client.ExitStatus.ExitStatus() != 0 {
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

func (r *Runner) Register(id int, reply *Empty) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	client, found := r.clients[id]
	if !found {
		return fmt.Errorf("client id %d not found", id)
	}
	if client.State == stateConnected {
		return fmt.Errorf("client id %d already registered", id)
	}
	client.State = stateConnected
	return nil
}

func (r *Runner) Status(id int, reply *Status) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	reply.AccessToken = r.conf.AccessToken

	if id == 0 {
		reply.Ready = true
	} else if client, found := r.clients[id-1]; found && client.State == stateExited {
		reply.Ready = true
	}
	return nil
}

type Client struct {
	ID         int
	SocketPath string
	client     *rpc.Client
}

var errNotConnected = errors.New("client not connected")

func (c *Client) Connect() error {
	if c.SocketPath == "" {
		c.SocketPath = defaultSocketPath
	}
	client, err := rpc.DialHTTP("unix", c.SocketPath)
	if err != nil {
		return err
	}
	c.client = client
	return c.client.Call("Runner.Register", c.ID, nil)
}

func (c *Client) Exit(exitStatus process.WaitStatus) error {
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

func (c *Client) WaitReady() <-chan WaitReadyResponse {
	result := make(chan WaitReadyResponse)
	go func() {
		for {
			var reply Status
			if err := c.client.Call("Runner.Status", c.ID, &reply); err != nil {
				result <- WaitReadyResponse{Err: err}
				return
			}
			if reply.Ready {
				result <- WaitReadyResponse{Status: reply}
				return
			}
			// TODO: configurable interval
			time.Sleep(time.Second)
		}
	}()
	return result
}

func (c *Client) Close() {
	c.client.Close()
}
