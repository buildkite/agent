package dockerexec

import (
	"context"
	"debug/elf"
	"fmt"
	"io"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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
