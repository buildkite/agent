package jobapi

import (
	"context"
	"encoding/json"
	"errors"
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
)

type fakeServer struct {
	env         map[string]string
	promised    []PromiseFailureRequest
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
	go func() {
		_ = f.svr.Serve(ln)
	}()
	return f, nil
}

func (f *fakeServer) Close() { _ = f.svr.Close() }

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		_ = socket.WriteError(w, "invalid Authorization header", http.StatusForbidden)
		return
	}

	if r.URL.Path == "/api/current-job/v0/promise-failure" {
		var req PromiseFailureRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			_ = socket.WriteError(w, fmt.Sprintf("decoding request: %v", err), http.StatusBadRequest)
			return
		}
		f.promised = append(f.promised, req)
		// Exit status 99 stands in for a declaration the Buildkite API rejects.
		if req.ExitStatus == 99 {
			_ = socket.WriteError(w, "a different exit status was already declared", http.StatusConflict)
			return
		}
		if err := json.NewEncoder(w).Encode(&PromiseFailureResponse{Outcome: PromiseFailureDeclared}); err != nil {
			_ = socket.WriteError(w, fmt.Sprintf("encoding response: %v", err), http.StatusInternalServerError)
		}
		return
	}

	if r.URL.Path != "/api/current-job/v0/env" {
		_ = socket.WriteError(w, fmt.Sprintf("not found: %q", r.URL.Path), http.StatusNotFound)
		return
	}

	switch r.Method {
	case "GET":
		b := EnvGetResponse{Env: f.env}
		if err := json.NewEncoder(w).Encode(&b); err != nil {
			_ = socket.WriteError(w, fmt.Sprintf("encoding response: %v", err), http.StatusInternalServerError)
		}

	case "PATCH":
		var req EnvUpdateRequestPayload
		var resp EnvUpdateResponse
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			_ = socket.WriteError(w, fmt.Sprintf("decoding request: %v", err), http.StatusBadRequest)
			return
		}
		for k, v := range req.Env {
			if k == "READONLY" {
				_ = socket.WriteError(w, "mutating READONLY is not allowed", http.StatusBadRequest)
				return
			}
			if v == nil {
				_ = socket.WriteError(w, fmt.Sprintf("setting %q to null is not allowed", k), http.StatusBadRequest)
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
			_ = socket.WriteError(w, fmt.Sprintf("encoding response: %v", err), http.StatusInternalServerError)
		}

	case "DELETE":
		var req EnvDeleteRequest
		var resp EnvDeleteResponse
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			_ = socket.WriteError(w, fmt.Sprintf("decoding request: %v", err), http.StatusBadRequest)
			return
		}
		for _, k := range req.Keys {
			if k == "READONLY" {
				_ = socket.WriteError(w, "deleting READONLY is not allowed", http.StatusBadRequest)
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
			_ = socket.WriteError(w, fmt.Sprintf("encoding response: %v", err), http.StatusInternalServerError)
		}

	default:
		_ = socket.WriteError(w, fmt.Sprintf("unsupported method %q", r.Method), http.StatusBadRequest)
	}
}

func TestClientDeclarePromiseFailure(t *testing.T) {
	t.Parallel()

	svr, err := runFakeServer()
	if err != nil {
		t.Fatalf("runFakeServer() = %v", err)
	}
	defer svr.Close()

	ctx, canc := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(canc)

	cli, err := NewClient(ctx, svr.sock, svr.token)
	if err != nil {
		t.Fatalf("NewClient(%q, %q) error = %v", svr.sock, svr.token, err)
	}

	// A successful declaration returns the outcome and no error, and forwards the
	// exit status and reason to the server.
	outcome, err := cli.DeclarePromiseFailure(ctx, 1, "tests failed")
	if err != nil {
		t.Fatalf("cli.DeclarePromiseFailure(1) error = %v", err)
	}
	if outcome != PromiseFailureDeclared {
		t.Errorf("cli.DeclarePromiseFailure(1) outcome = %q, want %q", outcome, PromiseFailureDeclared)
	}

	// A rejected declaration surfaces a socket.APIErr carrying the Buildkite API
	// status code, so the caller can produce an accurate message.
	_, err = cli.DeclarePromiseFailure(ctx, 99, "")
	var apiErr socket.APIErr
	if !errors.As(err, &apiErr) {
		t.Fatalf("cli.DeclarePromiseFailure(99) error = %v, want a socket.APIErr", err)
	}
	if apiErr.StatusCode != http.StatusConflict {
		t.Errorf("apiErr.StatusCode = %d, want %d", apiErr.StatusCode, http.StatusConflict)
	}

	want := []PromiseFailureRequest{
		{ExitStatus: 1, Reason: "tests failed"},
		{ExitStatus: 99},
	}
	if diff := cmp.Diff(want, svr.promised); diff != "" {
		t.Errorf("recorded promise-failure requests diff (-want +got):\n%s", diff)
	}
}

func TestClient_NoSocket(t *testing.T) {
	// t.Parallel() // Can't be parallelised, because it uses the t.Setenv() function

	ctx, canc := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(canc)

	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "") // This may be set if the test is being run by a buildkite agent!
	_, err := NewDefaultClient(ctx)
	if want := errNoJobAPISocketEnv; !errors.Is(err, want) {
		t.Fatalf("NewDefaultClient() error = %v, want %v", err, errNoJobAPISocketEnv)
	}
}

func TestClient_NoToken(t *testing.T) {
	// t.Parallel() // Can't be parallelised, because it uses the t.Setenv() function

	ctx, canc := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(canc)

	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "/tmp/fake-socket") // Just to make sure it's set
	t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", "")                  // This may be set if the test is being run by a buildkite agent!

	_, err := NewDefaultClient(ctx)
	if want := errNoJobAPITokenEnv; !errors.Is(err, want) {
		t.Fatalf("NewDefaultClient() error = %v, want %v", err, errNoJobAPITokenEnv)
	}
}

func TestClientEnvGet(t *testing.T) {
	t.Parallel()

	svr, err := runFakeServer()
	if err != nil {
		t.Fatalf("runFakeServer() = %v", err)
	}
	defer svr.Close()

	ctx, canc := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(canc)

	cli, err := NewClient(ctx, svr.sock, svr.token)
	if err != nil {
		t.Fatalf("NewClient(%q, %q) error = %v", svr.sock, svr.token, err)
	}

	got, err := cli.EnvGet(t.Context())
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

	ctx, canc := context.WithTimeout(t.Context(), 10*time.Second)
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

	got, err := cli.EnvUpdate(t.Context(), req)
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

	ctx, canc := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(canc)

	cli, err := NewClient(ctx, svr.sock, svr.token)
	if err != nil {
		t.Fatalf("NewClient(%q, %q) error = %v", svr.sock, svr.token, err)
	}

	req := []string{"YZMA"}
	got, err := cli.EnvDelete(t.Context(), req)
	if err != nil {
		t.Fatalf("cli.EnvUpdate() error = %v", err)
	}

	want := []string{"YZMA"}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("cli.EnvDelete diff (-got +want):\n%s", diff)
	}
}
