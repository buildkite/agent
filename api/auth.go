package api

import (
	"fmt"
	"net/http"
)

type canceler interface {
	CancelRequest(*http.Request)
}

// authenticatedTransport manages injection of the API token
type authenticatedTransport struct {
	// The Token used for authentication. This can either the be
	// organizations registration token, or the agents access token.
	Token string

	// Delegate is the underlying HTTP transport
	Delegate http.RoundTripper
}

// RoundTrip invoked each time a request is made
func (t authenticatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Token == "" {
		return nil, fmt.Errorf("Invalid token, empty string supplied")
	}

	req.Header.Set("Authorization", fmt.Sprintf("Token %s", t.Token))

	return t.Delegate.RoundTrip(req)
}

// CancelRequest cancels an in-flight request by closing its connection.
func (t *authenticatedTransport) CancelRequest(req *http.Request) {
	cancelableTransport := t.Delegate.(canceler)
	cancelableTransport.CancelRequest(req)
}
