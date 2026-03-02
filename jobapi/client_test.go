package jobapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/socket"
	"github.com/google/go-cmp/cmp"
	"gotest.tools/v3/assert"
)

type fakeServer struct {
	env         map[string]string
	sock, token string
	svr         *http.Server
}

func runFakeServer() (svr *fakeServer, err error) {
	f := &fakeServer{
		env: map[string]string{
			"KUZCO":    "Llama",
			"KRONK":    "Himbo",
			"YZMA":     "Villain",
			"READONLY": "Should never change",
		},
		sock:  filepath.Join(os.TempDir(), fmt.Sprintf("testsocket-%d-%x", os.Getpid(), rand.Int())),
		token: "to_the_secret_lab",
	}

	f.svr = &http.Server{Handler: f}

	ln, err := net.Listen("unix", f.sock)
	if err != nil {
		return nil, fmt.Errorf("net.Listen(unix, %q) error = %w", f.sock, err)
	}
	go f.svr.Serve(ln) //nolint:errcheck // returns ErrServerClosed on normal shutdown
	return f, nil
}

func (f *fakeServer) Close() { f.svr.Close() } //nolint:errcheck // best-effort shutdown in test

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		socket.WriteError(w, "invalid Authorization header", http.StatusForbidden) //nolint:errcheck // test handler
		return
	}
	if r.URL.Path != "/api/current-job/v0/env" {
		socket.WriteError(w, fmt.Sprintf("not found: %q", r.URL.Path), http.StatusNotFound) //nolint:errcheck // test handler
		return
	}

	switch r.Method {
	case "GET":
		b := EnvGetResponse{Env: f.env}
		if err := json.NewEncoder(w).Encode(&b); err != nil {
			socket.WriteError(w, fmt.Sprintf("encoding response: %v", err), http.StatusInternalServerError) //nolint:errcheck // test handler
		}

	case "PATCH":
		var req EnvUpdateRequestPayload
		var resp EnvUpdateResponse
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			socket.WriteError(w, fmt.Sprintf("decoding request: %v", err), http.StatusBadRequest) //nolint:errcheck // test handler
			return
		}
		for k, v := range req.Env {
			if k == "READONLY" {
				socket.WriteError(w, "mutating READONLY is not allowed", http.StatusBadRequest) //nolint:errcheck // test handler
				return
			}
			if v == nil {
				socket.WriteError(w, fmt.Sprintf("setting %q to null is not allowed", k), http.StatusBadRequest) //nolint:errcheck // test handler
				return
			}
		}
		for k, v := range req.Env {
			if _, ok := f.env[k]; ok {
				resp.Updated = append(resp.Updated, k)
			} else {
				resp.Added = append(resp.Added, k)
			}
			f.env[k] = *v
		}
		resp.Normalize()
		if err := json.NewEncoder(w).Encode(&resp); err != nil {
			socket.WriteError(w, fmt.Sprintf("encoding response: %v", err), http.StatusInternalServerError) //nolint:errcheck // test handler
		}

	case "DELETE":
		var req EnvDeleteRequest
		var resp EnvDeleteResponse
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			socket.WriteError(w, fmt.Sprintf("decoding request: %v", err), http.StatusBadRequest) //nolint:errcheck // test handler
			return
		}
		for _, k := range req.Keys {
			if k == "READONLY" {
				socket.WriteError(w, "deleting READONLY is not allowed", http.StatusBadRequest) //nolint:errcheck // test handler
			}
		}
		for _, k := range req.Keys {
			if _, ok := f.env[k]; !ok {
				continue
			}
			resp.Deleted = append(resp.Deleted, k)
			delete(f.env, k)
		}
		resp.Normalize()
		if err := json.NewEncoder(w).Encode(&resp); err != nil {
			socket.WriteError(w, fmt.Sprintf("encoding response: %v", err), http.StatusInternalServerError) //nolint:errcheck // test handler
		}

	default:
		socket.WriteError(w, fmt.Sprintf("unsupported method %q", r.Method), http.StatusBadRequest) //nolint:errcheck // test handler
	}
}

func TestClient_NoSocket(t *testing.T) {
	// t.Parallel() // Can't be parallelised, because it uses the t.Setenv() function

	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "") // This may be set if the test is being run by a buildkite agent!
	_, err := NewDefaultClient(ctx)
	assert.ErrorIs(t, err, errNoJobAPISocketEnv, "NewDefaultClient() error = %v, want %v", err, errNoJobAPISocketEnv)
}

func TestClient_NoToken(t *testing.T) {
	// t.Parallel() // Can't be parallelised, because it uses the t.Setenv() function

	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "/tmp/fake-socket") // Just to make sure it's set
	t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", "")                  // This may be set if the test is being run by a buildkite agent!

	_, err := NewDefaultClient(ctx)
	assert.ErrorIs(t, err, errNoJobAPITokenEnv, "NewDefaultClient() error = %v, want %v", err, errNoJobAPITokenEnv)
}

func TestClientEnvGet(t *testing.T) {
	t.Parallel()

	svr, err := runFakeServer()
	if err != nil {
		t.Fatalf("runFakeServer() = %v", err)
	}
	defer svr.Close()

	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	cli, err := NewClient(ctx, svr.sock, svr.token)
	if err != nil {
		t.Fatalf("NewClient(%q, %q) error = %v", svr.sock, svr.token, err)
	}

	got, err := cli.EnvGet(context.Background())
	if err != nil {
		t.Fatalf("cli.EnvGet() error = %v", err)
	}

	want := map[string]string{
		"KUZCO":    "Llama",
		"KRONK":    "Himbo",
		"YZMA":     "Villain",
		"READONLY": "Should never change",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("cli.EnvGet diff (-got +want):\n%s", diff)
	}
}

func TestClientEnvUpdate(t *testing.T) {
	t.Parallel()

	svr, err := runFakeServer()
	if err != nil {
		t.Fatalf("runFakeServer() = %v", err)
	}
	defer svr.Close()

	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	cli, err := NewClient(ctx, svr.sock, svr.token)
	if err != nil {
		t.Fatalf("NewClient(%q, %q) error = %v", svr.sock, svr.token, err)
	}

	req := &EnvUpdateRequest{
		Env: map[string]string{
			"PACHA": "Friend",
			"YZMA":  "Kitten",
		},
	}

	got, err := cli.EnvUpdate(context.Background(), req)
	if err != nil {
		t.Fatalf("cli.EnvUpdate() error = %v", err)
	}

	want := &EnvUpdateResponse{
		Added:   []string{"PACHA"},
		Updated: []string{"YZMA"},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("cli.EnvUpdate diff (-got +want):\n%s", diff)
	}
}

func TestClientEnvDelete(t *testing.T) {
	t.Parallel()

	svr, err := runFakeServer()
	if err != nil {
		t.Fatalf("runFakeServer() = %v", err)
	}
	defer svr.Close()

	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	cli, err := NewClient(ctx, svr.sock, svr.token)
	if err != nil {
		t.Fatalf("NewClient(%q, %q) error = %v", svr.sock, svr.token, err)
	}

	req := []string{"YZMA"}
	got, err := cli.EnvDelete(context.Background(), req)
	if err != nil {
		t.Fatalf("cli.EnvUpdate() error = %v", err)
	}

	want := []string{"YZMA"}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("cli.EnvDelete diff (-got +want):\n%s", diff)
	}
}
