package agenthttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthenticatedClientDoesNotFollowCrossOriginRedirect(t *testing.T) {
	redirectTargetReached := make(chan string, 1)
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectTargetReached <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(redirectTarget.Close)

	redirectSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	t.Cleanup(redirectSource.Close)

	client := NewClient(WithAuthToken("secret-token"))
	resp, err := client.Get(redirectSource.URL)
	if err != nil {
		t.Fatalf("client.Get(%q) error = %v", redirectSource.URL, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if got, want := resp.StatusCode, http.StatusFound; got != want {
		t.Fatalf("response status = %d, want %d", got, want)
	}

	select {
	case got := <-redirectTargetReached:
		t.Fatalf("cross-origin redirect target received Authorization header %q", got)
	default:
	}
}

func TestAuthenticatedClientFollowsSameOriginRedirect(t *testing.T) {
	finalAuthorization := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/final":
			finalAuthorization <- r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient(WithAuthBearer("secret-bearer"))
	resp, err := client.Get(server.URL + "/redirect")
	if err != nil {
		t.Fatalf("client.Get(%q) error = %v", server.URL+"/redirect", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if got, want := resp.StatusCode, http.StatusNoContent; got != want {
		t.Fatalf("response status = %d, want %d", got, want)
	}

	select {
	case got := <-finalAuthorization:
		if want := "Bearer secret-bearer"; got != want {
			t.Fatalf("final Authorization header = %q, want %q", got, want)
		}
	default:
		t.Fatal("same-origin redirect target was not reached")
	}
}

func TestUnauthenticatedClientFollowsCrossOriginRedirect(t *testing.T) {
	redirectTargetReached := make(chan struct{}, 1)
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectTargetReached <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(redirectTarget.Close)

	redirectSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	t.Cleanup(redirectSource.Close)

	client := NewClient()
	resp, err := client.Get(redirectSource.URL)
	if err != nil {
		t.Fatalf("client.Get(%q) error = %v", redirectSource.URL, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if got, want := resp.StatusCode, http.StatusNoContent; got != want {
		t.Fatalf("response status = %d, want %d", got, want)
	}

	select {
	case <-redirectTargetReached:
	default:
		t.Fatal("cross-origin redirect target was not reached")
	}
}
