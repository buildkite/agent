package api

import (
	"fmt"
	"net/http"
)

// authenticatedTransport manages injection of the API token.
type authenticatedTransport struct {
	// The Token used for authentication. This can either the be
	// organizations registration token, or the agents access token.
	Token string

	// Delegate is the underlying HTTP transport
	Delegate http.RoundTripper
}

// RoundTrip invoked each time a request is made.
func (t authenticatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Per net/http#RoundTripper:
	//
	// "RoundTrip must always close the body, including on errors, ..."
	reqBodyClosed := false
	if req.Body != nil {
		defer func() {
			if !reqBodyClosed {
				req.Body.Close()
			}
		}()
	}

	if t.Token == "" {
		return nil, fmt.Errorf("Invalid token, empty string supplied")
	}

	// Per net/http#RoundTripper:
	//
	// "RoundTrip should not modify the request, except for
	// consuming and closing the Request's Body. RoundTrip may
	// read fields of the request in a separate goroutine. Callers
	// should not mutate or reuse the request until the Response's
	// Body has been closed."
	//
	// But we can pass a _different_ request to t.Delegate.RoundTrip.
	// req.Clone does a sufficiently deep clone (including Header which we
	// modify).
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", fmt.Sprintf("Token %s", t.Token))

	// req.Body is assumed to be closed by the delegate.
	reqBodyClosed = true
	return t.Delegate.RoundTrip(req)
}

// CancelRequest forwards the call to t.Delegate, if it implements CancelRequest
// itself.
func (t *authenticatedTransport) CancelRequest(req *http.Request) {
	canceler, ok := t.Delegate.(interface{ CancelRequest(*http.Request) })
	if !ok {
		return
	}
	canceler.CancelRequest(req)
}

// CloseIdleConnections forwards the call to t.Delegate, if it implements
// CloseIdleConnections itself.
func (t *authenticatedTransport) CloseIdleConnections() {
	closer, ok := t.Delegate.(interface{ CloseIdleConnections() })
	if !ok {
		return
	}
	closer.CloseIdleConnections()
}
