package socket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
)

// Error response is the response body for any errors that occur
type ErrorResponse struct {
	Error string `json:"error"`
}

// APIErr is an error type used for API error responses (the API returned a
// well-formed JSON ErrorResponse and a non-200 status code).
type APIErr struct {
	Msg        string
	StatusCode int
}

func (e APIErr) Error() string {
	return fmt.Sprintf("API status %d: %s", e.StatusCode, e.Msg)
}

// Client is a client for a HTTP-over-Unix Domain Socket API.
type Client struct {
	cli   *http.Client
	token string
}

// NewClient creates a new Client. The context is used for an internal check
// that the socket can be dialled.
func NewClient(ctx context.Context, path, token string) (*Client, error) {
	// Check the socket path exists and is a socket.
	// Note that os.ModeSocket might not be set on Windows.
	// (https://github.com/golang/go/issues/33357)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat socket: %w", err)
		}
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%q is not a socket", path)
		}
	}

	dialer := net.Dialer{}

	// Try to connect to the socket.
	test, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("socket test connection: %w", err)
	}
	test.Close()

	return &Client{
		cli: &http.Client{
			Transport: &http.Transport{
				// Ignore arguments, dial socket
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return dialer.DialContext(ctx, "unix", path)
				},
			},
		},
		token: token,
	}, nil
}

// Do implements the common bits of an API call. req is serialised to JSON and
// passed as the request body if not nil. The method is called, with the token
// added in the Authorization header. The response is deserialised, either into
// the object passed into resp if the status is 200 OK, otherwise into an error.
func (c *Client) Do(ctx context.Context, method, url string, req, resp any) error {
	var body io.Reader
	if req != nil {
		buf, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshalling request: %w", err)
		}
		body = bytes.NewReader(buf)
	}

	hreq, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("creating a request: %w", err)
	}
	if c.token != "" {
		hreq.Header.Set("Authorization", "Bearer "+c.token)
	}

	hresp, err := c.cli.Do(hreq)
	if err != nil {
		return err
	}
	defer hresp.Body.Close()
	dec := json.NewDecoder(hresp.Body)

	switch hresp.StatusCode / 100 {
	case 4, 5:
		var er ErrorResponse
		if err := dec.Decode(&er); err != nil {
			return fmt.Errorf("decoding error response: %w", err)
		}
		return APIErr{
			Msg:        er.Error,
			StatusCode: hresp.StatusCode,
		}
	}

	if resp == nil {
		return nil
	}
	if err := dec.Decode(resp); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}
