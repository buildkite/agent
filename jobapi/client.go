package jobapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
)

const envURL = "http://job/api/current-job/v0/env"

// Client connects to the Job API.
type Client struct {
	client *http.Client
	token  string
}

// NewDefaultClient returns a new Client with the default socket path and token.
func NewDefaultClient() (*Client, error) {
	sock, token, err := DefaultSocketPath()
	if err != nil {
		return nil, err
	}

	return NewClient(sock, token)
}

// DefaultSocketPath returns the socket path and access token, if available.
func DefaultSocketPath() (path, token string, err error) {
	path = os.Getenv("BUILDKITE_AGENT_JOB_API_SOCKET")
	if path == "" {
		return "", "", errors.New("BUILDKITE_AGENT_JOB_API_SOCKET empty or undefined")
	}
	token = os.Getenv("BUILDKITE_AGENT_JOB_API_TOKEN")
	if token == "" {
		return "", "", errors.New("BUILDKITE_AGENT_JOB_API_TOKEN empty or undefined")
	}
	return path, token, nil
}

// NewClient creates a new Client.
func NewClient(sock, token string) (*Client, error) {
	// Check the socket path exists and is a socket.
	// Note that os.ModeSocket might not be set on Windows.
	// (https://github.com/golang/go/issues/33357)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(sock)
		if err != nil {
			return nil, fmt.Errorf("stat socket: %w", err)
		}
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%q is not a socket", sock)
		}
	}

	// Try to connect to the socket.
	test, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("socket test connection: %w", err)
	}
	test.Close()

	dialer := net.Dialer{}
	return &Client{
		client: &http.Client{
			Transport: &http.Transport{
				// Ignore arguments, dial socket
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return dialer.DialContext(ctx, "unix", sock)
				},
			},
		},
		token: token,
	}, nil
}

// do implements the common bits of an API call. req is serialised to JSON and
// passed as the request body if not nil. The method is called, with the token
// added in the Authorization header. The response is deserialised, either into
// the object passed into resp if the status is 200 OK, otherwise into an error.
func (c *Client) do(ctx context.Context, method, url string, req, resp any) error {
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

	hreq.Header.Set("Authorization", "Bearer "+c.token)
	hresp, err := c.client.Do(hreq)
	if err != nil {
		return err
	}
	defer hresp.Body.Close()
	dec := json.NewDecoder(hresp.Body)

	if hresp.StatusCode != 200 {
		var er ErrorResponse
		if err := dec.Decode(&er); err != nil {
			return fmt.Errorf("decoding error response: %w", err)
		}
		return fmt.Errorf("error from job executor: %s", er.Error)
	}

	if resp == nil {
		return nil
	}
	if err := dec.Decode(resp); err != nil {
		return fmt.Errorf("decoding response: %w:", err)
	}
	return nil
}

// EnvGet gets the current environment variables from within the job executor.
func (c *Client) EnvGet(ctx context.Context) (map[string]string, error) {
	var resp EnvGetResponse
	if err := c.do(ctx, "GET", envURL, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Env, nil
}

// EnvUpdate updates environment variables within the job executor.
func (c *Client) EnvUpdate(ctx context.Context, req *EnvUpdateRequest) (*EnvUpdateResponse, error) {
	var resp EnvUpdateResponse
	if err := c.do(ctx, "PATCH", envURL, req, &resp); err != nil {
		return nil, err
	}
	resp.Normalize()
	return &resp, nil
}

// EnvDelete deletes environment variables within the job executor.
func (c *Client) EnvDelete(ctx context.Context, del []string) (deleted []string, err error) {
	req := EnvDeleteRequest{
		Keys: del,
	}
	var resp EnvDeleteResponse
	if err := c.do(ctx, "DELETE", envURL, &req, &resp); err != nil {
		return nil, err
	}
	resp.Normalize()
	return resp.Deleted, nil
}
