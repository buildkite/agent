package kubernetes

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"syscall"

	"github.com/buildkite/agent/v3/logger"
)

const defaultSocketPath = "/workspace/buildkite.sock"

func New(l logger.Logger, c Config) *Runner {
	if c.SocketPath == "" {
		c.SocketPath = defaultSocketPath
	}
	return &Runner{
		logger: l,
		conf:   c,
	}
}

type Runner struct {
	logger        logger.Logger
	conf          Config
	mu            sync.Mutex
	listener      net.Listener
	started, done chan struct{}
	waitStatus    syscall.WaitStatus
	once          sync.Once
}

type Config struct {
	SocketPath     string
	Env            []string
	Stdout, Stderr io.Writer
}

func (r *Runner) Run(ctx context.Context) error {
	rpc.Register(r)
	rpc.HandleHTTP()
	l, err := net.Listen("unix", r.conf.SocketPath)
	if err != nil {
		log.Fatal("listen error:", err)
	}
	defer l.Close()
	defer os.Remove(r.conf.SocketPath)
	r.listener = l
	go http.Serve(l, nil)

	r.mu.Lock()
	if r.done == nil {
		r.done = make(chan struct{})
	}
	if r.started == nil {
		r.started = make(chan struct{})
	}
	r.mu.Unlock()
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

func (r *Runner) WaitStatus() syscall.WaitStatus {
	return r.waitStatus
}

// ==== sidecar api ====

type Empty struct{}
type Logs struct {
	Data []byte
}

type ExitCode struct {
	ExitStatus syscall.WaitStatus
}

func (t *Runner) WriteLogs(args Logs, reply *Empty) error {
	t.once.Do(func() {
		close(t.started)
	})
	_, err := io.Copy(t.conf.Stdout, bytes.NewReader(args.Data))
	return err
}

func (t *Runner) Exit(args ExitCode, reply *Empty) error {
	t.waitStatus = args.ExitStatus
	close(t.done)
	return nil
}

type Client struct {
	SocketPath string
	client     *rpc.Client
}

func (c *Client) Connect() error {
	if c.SocketPath == "" {
		c.SocketPath = defaultSocketPath
	}
	client, err := rpc.DialHTTP("unix", c.SocketPath)
	if err != nil {
		return err
	}
	c.client = client
	return nil
}

func (c *Client) Exit(exitStatus syscall.WaitStatus) error {
	if c.client == nil {
		return nil
	}
	return c.client.Call("Runner.Exit", ExitCode{
		ExitStatus: exitStatus,
	}, nil)
}

// Write implements io.Writer
func (c *Client) Write(p []byte) (int, error) {
	if c.client == nil {
		return 0, nil
	}
	n := len(p)
	err := c.client.Call("Runner.WriteLogs", Logs{
		Data: p,
	}, nil)
	return n, err
}
