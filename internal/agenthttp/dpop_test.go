package agenthttp_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/buildkite/agent/v4/internal/agenthttp"
	"github.com/buildkite/agent/v4/internal/identity"
)

func TestDPoPRedirectsStayOnAPIOrigin(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := identity.New(private)
	if err != nil {
		t.Fatal(err)
	}
	var escaped atomic.Bool
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		escaped.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/other":
			http.Redirect(w, r, other.URL, http.StatusFound)
		default:
			if r.Header.Get("Authorization") != "DPoP session" || r.Header.Get("DPoP") == "" {
				http.Error(w, "no proof", http.StatusUnauthorized)
			}
		}
	}))
	defer server.Close()
	client := agenthttp.WithDPoP(server.Client(), "session", key, server.URL, true)
	resp, err := client.Get(server.URL + "/same")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("same-origin redirect failed: %s", resp.Status)
	}
	if resp, err := client.Get(server.URL + "/other"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("cross-origin redirect was followed")
	}
	if escaped.Load() {
		t.Fatal("request escaped the configured API origin")
	}
	if resp, err := client.Get(strings.Replace(server.URL, "https:", "http:", 1)); err == nil {
		_ = resp.Body.Close()
		t.Fatal("HTTP downgrade was accepted")
	}
}

func TestDPoPNonceRetriesAreBoundedAndReplayTheBody(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := identity.New(private)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != "important request body" {
			t.Errorf("nonce retry lost request body: %q, %v", body, err)
		}
		count := calls.Add(1)
		w.Header().Set("DPoP-Nonce", fmt.Sprintf("nonce-%d", count))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client := agenthttp.WithDPoP(server.Client(), "session", key, server.URL, true)
	for _, replayable := range []bool{true, false} {
		calls.Store(0)
		req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("important request body"))
		if err != nil {
			t.Fatal(err)
		}
		want := int32(2)
		if !replayable {
			req.GetBody = nil
			want = 1
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized || calls.Load() != want {
			t.Fatalf("replayable=%v: status=%d requests=%d, want 401 and %d", replayable, resp.StatusCode, calls.Load(), want)
		}
	}
}
