package agent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIProxy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(`Authorization`) != `Token llamas` {
			http.Error(w, "Invalid authorization token", http.StatusUnauthorized)
			return
		}
		fmt.Fprintln(w, `{"message": "ok"}`)
	}))
	defer ts.Close()

	// create proxy to our fake api
	proxy := NewAPIProxy(ts.URL, `llamas`)
	go proxy.Listen()
	proxy.Wait()
	defer proxy.Close()

	// create a client to talk to our proxy api
	client := APIClient{
		Endpoint: proxy.Endpoint(),
		Token:    proxy.AccessToken(),
	}.Create()

	// fire a ping via the proxy
	p, _, err := client.Pings.Get()
	if err != nil {
		t.Fatal(err)
	}

	if p.Message != `ok` {
		t.Fatalf("Expected message to be `ok`, got %q", p.Message)
	}
}

func TestAPIProxyFailsWithoutAccessToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(`Authorization`) != `Token llamas` {
			http.Error(w, "Invalid authorization token", http.StatusUnauthorized)
			return
		}
		fmt.Fprintln(w, `{"message": "ok"}`)
	}))
	defer ts.Close()

	// create proxy to our fake api
	proxy := NewAPIProxy(ts.URL, `llamas`)
	go proxy.Listen()
	proxy.Wait()
	defer proxy.Close()

	// create a client to talk to our proxy api, but with incorrect access token
	client := APIClient{
		Endpoint: proxy.Endpoint(),
		Token:    `xxx`,
	}.Create()

	// fire a ping via the proxy
	_, _, err := client.Pings.Get()
	if err == nil {
		t.Fatalf("Expected an error without an access token")
	}
}
