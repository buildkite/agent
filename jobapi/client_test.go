package jobapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
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
	go f.svr.Serve(ln)
	return f, nil
}

func (f *fakeServer) Close() { f.svr.Close() }

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		writeError(w, "invalid Authorization header", http.StatusForbidden)
		return
	}
	if r.URL.Path != "/api/current-job/v0/env" {
		writeError(w, fmt.Sprintf("not found: %q", r.URL.Path), http.StatusNotFound)
		return
	}

	switch r.Method {
	case "GET":
		b := EnvGetResponse{Env: f.env}
		if err := json.NewEncoder(w).Encode(&b); err != nil {
			writeError(w, fmt.Sprintf("encoding response: %v", err), http.StatusInternalServerError)
		}

	case "PATCH":
		var req EnvUpdateRequest
		var resp EnvUpdateResponse
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, fmt.Sprintf("decoding request: %v", err), http.StatusBadRequest)
			return
		}
		for k, v := range req.Env {
			if k == "READONLY" {
				writeError(w, "mutating READONLY is not allowed", http.StatusBadRequest)
				return
			}
			if v == nil {
				writeError(w, fmt.Sprintf("setting %q to null is not allowed", k), http.StatusBadRequest)
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
			writeError(w, fmt.Sprintf("encoding response: %v", err), http.StatusInternalServerError)
		}

	case "DELETE":
		var req EnvDeleteRequest
		var resp EnvDeleteResponse
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, fmt.Sprintf("decoding request: %v", err), http.StatusBadRequest)
			return
		}
		for _, k := range req.Keys {
			if k == "READONLY" {
				writeError(w, "deleting READONLY is not allowed", http.StatusBadRequest)
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
			writeError(w, fmt.Sprintf("encoding response: %v", err), http.StatusInternalServerError)
		}

	default:
		writeError(w, fmt.Sprintf("unsupported method %q", r.Method), http.StatusBadRequest)
	}
}

func TestClient_NoSocket(t *testing.T) {
	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	if _, err := NewDefaultClient(ctx); err == nil {
		t.Errorf("NewDefaultClient(ctx) error = %v, want nil", err)
	}
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

	sp := func(s string) *string { return &s }
	req := &EnvUpdateRequest{
		Env: map[string]*string{
			"PACHA": sp("Friend"),
			"YZMA":  sp("Kitten"),
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
