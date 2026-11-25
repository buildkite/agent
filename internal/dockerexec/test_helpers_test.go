package dockerexec

import (
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"iter"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/client"
)

// fakeClient is a scriptable dockerClient that records the calls it gets.
// A nil function field makes that call fail the test.
type fakeClient struct {
	t *testing.T

	ping         func() error
	imagePull    func(ref string, opts client.ImagePullOptions) error
	imageInspect func(ref string) (client.ImageInspectResult, error)

	containerCreate  func(ctx context.Context, opts client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	containerAttach  func(ctx context.Context, id string) (client.ContainerAttachResult, error)
	containerWait    func(ctx context.Context, id string) client.ContainerWaitResult
	containerStart   func(ctx context.Context, id string) error
	containerKill    func(id, signal string) error
	containerInspect func(id string) (client.ContainerInspectResult, error)
	containerRemove  func(ref string, opts client.ContainerRemoveOptions) error

	mu    sync.Mutex
	calls []string
}

func (f *fakeClient) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

// Calls returns the names of the calls made so far, in order.
func (f *fakeClient) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeClient) Ping(context.Context, client.PingOptions) (client.PingResult, error) {
	f.record("Ping")
	if f.ping == nil {
		f.t.Error("unexpected Ping")
		return client.PingResult{}, nil
	}
	return client.PingResult{}, f.ping()
}

func (f *fakeClient) ImagePull(_ context.Context, ref string, opts client.ImagePullOptions) (client.ImagePullResponse, error) {
	f.record("ImagePull")
	if f.imagePull == nil {
		f.t.Error("unexpected ImagePull")
		return fakePullResponse{}, nil
	}
	// The real client reports most pull failures from Wait, after the
	// request succeeds, so the fake does the same.
	return fakePullResponse{err: f.imagePull(ref, opts)}, nil
}

func (f *fakeClient) ImageInspect(_ context.Context, ref string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	f.record("ImageInspect")
	if f.imageInspect == nil {
		f.t.Error("unexpected ImageInspect")
		return client.ImageInspectResult{}, nil
	}
	return f.imageInspect(ref)
}

func (f *fakeClient) ContainerCreate(ctx context.Context, opts client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	f.record("ContainerCreate")
	if f.containerCreate == nil {
		f.t.Error("unexpected ContainerCreate")
		return client.ContainerCreateResult{}, errors.New("unexpected")
	}
	return f.containerCreate(ctx, opts)
}

func (f *fakeClient) ContainerAttach(ctx context.Context, id string, _ client.ContainerAttachOptions) (client.ContainerAttachResult, error) {
	f.record("ContainerAttach")
	if f.containerAttach == nil {
		f.t.Error("unexpected ContainerAttach")
		return client.ContainerAttachResult{}, errors.New("unexpected")
	}
	return f.containerAttach(ctx, id)
}

func (f *fakeClient) ContainerWait(ctx context.Context, id string, _ client.ContainerWaitOptions) client.ContainerWaitResult {
	f.record("ContainerWait")
	if f.containerWait == nil {
		f.t.Error("unexpected ContainerWait")
		errC := make(chan error, 1)
		errC <- errors.New("unexpected")
		return client.ContainerWaitResult{Error: errC}
	}
	return f.containerWait(ctx, id)
}

func (f *fakeClient) ContainerStart(ctx context.Context, id string, _ client.ContainerStartOptions) (client.ContainerStartResult, error) {
	f.record("ContainerStart")
	if f.containerStart == nil {
		f.t.Error("unexpected ContainerStart")
		return client.ContainerStartResult{}, errors.New("unexpected")
	}
	return client.ContainerStartResult{}, f.containerStart(ctx, id)
}

func (f *fakeClient) ContainerKill(_ context.Context, id string, opts client.ContainerKillOptions) (client.ContainerKillResult, error) {
	f.record("ContainerKill " + opts.Signal)
	if f.containerKill == nil {
		f.t.Error("unexpected ContainerKill")
		return client.ContainerKillResult{}, errors.New("unexpected")
	}
	return client.ContainerKillResult{}, f.containerKill(id, opts.Signal)
}

func (f *fakeClient) ContainerInspect(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	f.record("ContainerInspect")
	if f.containerInspect == nil {
		f.t.Error("unexpected ContainerInspect")
		return client.ContainerInspectResult{}, errors.New("unexpected")
	}
	return f.containerInspect(id)
}

func (f *fakeClient) ContainerRemove(_ context.Context, ref string, opts client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	f.record("ContainerRemove " + ref)
	if f.containerRemove == nil {
		f.t.Error("unexpected ContainerRemove")
		return client.ContainerRemoveResult{}, errors.New("unexpected")
	}
	return client.ContainerRemoveResult{}, f.containerRemove(ref, opts)
}

// fakeContainer simulates one container behind a fakeClient: it writes
// output to the attach stream once started, and exits when a code is sent
// to exit. By default SIGKILL makes it exit 137 and other signals are
// ignored.
type fakeContainer struct {
	output []byte
	exit   chan int
	onKill func(signal string) error

	server  net.Conn
	written chan struct{}
}

// newFakeDaemon returns a fakeClient wired to a fakeContainer with ID "cid".
func newFakeDaemon(t *testing.T) (*fakeClient, *fakeContainer) {
	t.Helper()
	c := &fakeContainer{exit: make(chan int, 1), written: make(chan struct{})}
	c.onKill = func(signal string) error {
		if signal == "SIGKILL" {
			c.exitWith(137)
		}
		return nil
	}
	f := &fakeClient{t: t}
	f.containerCreate = func(context.Context, client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
		return client.ContainerCreateResult{ID: "cid"}, nil
	}
	f.containerAttach = func(context.Context, string) (client.ContainerAttachResult, error) {
		clientConn, serverConn := net.Pipe()
		c.server = serverConn
		return client.ContainerAttachResult{HijackedResponse: client.NewHijackedResponse(clientConn, "")}, nil
	}
	f.containerWait = func(ctx context.Context, _ string) client.ContainerWaitResult {
		// Unbuffered, like the real client's.
		resultC := make(chan container.WaitResponse)
		errC := make(chan error, 1)
		go func() {
			select {
			case code := <-c.exit:
				<-c.written
				_ = c.server.Close()
				resultC <- container.WaitResponse{StatusCode: int64(code)}
			case <-ctx.Done():
				errC <- ctx.Err()
			}
		}()
		return client.ContainerWaitResult{Result: resultC, Error: errC}
	}
	f.containerStart = func(context.Context, string) error {
		go func() {
			defer close(c.written)
			_, _ = c.server.Write(c.output)
		}()
		return nil
	}
	f.containerKill = func(_, signal string) error { return c.onKill(signal) }
	f.containerRemove = func(string, client.ContainerRemoveOptions) error { return nil }
	return f, c
}

// exitWith makes the container exit with code, if it has not already.
func (c *fakeContainer) exitWith(code int) {
	select {
	case c.exit <- code:
	default:
	}
}

// fakePullResponse is a finished pull whose Wait returns err.
type fakePullResponse struct{ err error }

func (fakePullResponse) Read([]byte) (int, error) { return 0, io.EOF }
func (fakePullResponse) Close() error             { return nil }
func (r fakePullResponse) Wait(context.Context) error {
	return r.err
}

func (fakePullResponse) JSONMessages(context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(func(jsonstream.Message, error) bool) {}
}

// staticBinary returns a static Linux binary that exits 0. It is built once
// per test run.
func staticBinary(t *testing.T) string {
	t.Helper()
	path, err := buildStaticBinaryOnce()
	if err != nil {
		t.Fatalf("building static binary: %v", err)
	}
	return path
}

// staticBinaryDir holds the shared static binary, for TestMain to remove.
var staticBinaryDir string

var buildStaticBinaryOnce = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp("", "dockerexec-test-")
	if err != nil {
		return "", err
	}
	staticBinaryDir = dir
	return buildStaticBinary(dir, "package main\n\nfunc main() {}\n")
})

// buildStaticBinary compiles the Go program src in dir with cgo disabled and
// returns the path of the static Linux binary.
func buildStaticBinary(dir, src string) (string, error) {
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fake\n\ngo 1.21\n"), 0o600); err != nil {
		return "", err
	}
	out := filepath.Join(dir, "fake-agent")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOFLAGS=")
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return out, nil
}

// dynamicBinary returns a dynamically linked binary from the host, or skips
// the test if the host has none (for example, a static busybox /bin/sh).
func dynamicBinary(t *testing.T) string {
	t.Helper()
	f, err := elf.Open("/bin/sh")
	if err != nil {
		t.Skipf("no ELF /bin/sh: %v", err)
	}
	defer f.Close() //nolint:errcheck // Read-only file.
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			return "/bin/sh"
		}
	}
	t.Skip("/bin/sh is statically linked")
	return ""
}

// writeDockerConfig writes a Docker CLI config.json into dir.
func writeDockerConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	if staticBinaryDir != "" {
		_ = os.RemoveAll(staticBinaryDir)
	}
	os.Exit(code)
}
