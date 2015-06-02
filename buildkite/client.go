package buildkite

import (
	"bytes"
	_ "crypto/sha512" // import sha512 to make sha512 ssl certs work
	"encoding/json"
	"errors"
	bkhttp "github.com/buildkite/agent/buildkite/http"
	"github.com/buildkite/agent/logger"
	"io"
	"net/http"
	"runtime"
	"strings"
)

type Client struct {
	// The URL of the Buildkite Agent API to communicate with. Defaults to
	// "https://agent.buildkite.com/v2".
	URL string

	// The authorization token agent being used to make API requests
	AuthorizationToken string

	// UserAgent to be provided in API requests. Set to DefaultUserAgent if not
	// specified.
	UserAgent string
}

func (c *Client) GetSession() *bkhttp.Session {
	session := new(bkhttp.Session)
	session.Endpoint = c.URL
	session.UserAgent = c.UserAgent
	session.Headers = []bkhttp.Header{
		bkhttp.Header{
			Name:  "Authorization",
			Value: "Token " + c.AuthorizationToken,
		},
	}

	return session
}

func (c *Client) Get(v interface{}, path string) error {
	return c.APIReq(v, "GET", path, nil)
}

func (c *Client) Put(v interface{}, path string, body interface{}) error {
	return c.APIReq(v, "PUT", path, body)
}

func (c *Client) Post(v interface{}, path string, body interface{}) error {
	return c.APIReq(v, "POST", path, body)
}

// Sends a Buildkite API request and decodes the response into v.
func (c *Client) APIReq(v interface{}, method string, path string, body interface{}) error {
	// Generate a new request
	req, err := c.NewRequest(method, path, body)
	if err != nil {
		return err
	}

	// Perform the request
	return c.DoReq(req, v)
}

// Generates an HTTP request for the Buildkite API, but does not
// perform the request.
func (c *Client) NewRequest(method string, path string, body interface{}) (*http.Request, error) {
	// Populate the request body if we have to
	var requestBody io.Reader
	var contentType string

	if body != nil {
		j, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}

		requestBody = bytes.NewReader(j)
		contentType = "application/json"
	}

	// Generate the URL for the request
	endpointUrl := strings.TrimRight(c.URL, "/")
	if endpointUrl == "" {
		endpointUrl = defaultAPIURL()
	}

	normalizedPath := strings.TrimLeft(path, "/")
	url := endpointUrl + "/" + normalizedPath

	// Create a new request object
	req, err := http.NewRequest(method, url, requestBody)
	if err != nil {
		return nil, err
	}

	// Set the accept content type. The Buildkite API only speaks
	// json.
	req.Header.Set("Accept", "application/json")

	// Set the authorization header
	req.Header.Set("Authorization", "Token "+c.AuthorizationToken)

	// Figure out and set the User Agent
	userAgent := c.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent()
	}

	req.Header.Set("User-Agent", userAgent)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return req, nil
}

// Submits an HTTP request, checks its response, and deserializes
// the response into v.
func (c *Client) DoReq(req *http.Request, v interface{}) error {
	logger.Debug("%s %s", req.Method, req.URL)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	// Be sure to close the response body at the end of
	// this function
	defer res.Body.Close()

	// Check the response of the response
	if err = checkResp(res); err != nil {
		return err
	}

	// body, err := ioutil.ReadAll(res.Body)
	// logger.Debug("%s", body)

	// Decode the response
	return json.NewDecoder(res.Body).Decode(v)
}

type errorResp struct {
	Message string `json:"message"`
}

func defaultUserAgent() string {
	return "buildkite-agent/" + Version() + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
}

func defaultAPIURL() string {
	return "https://agent.buildkite.com/v2"
}

func checkResp(res *http.Response) error {
	// Was the request not a: 200, 201, 202, etc
	if res.StatusCode/100 != 2 {
		// Decode the error json
		var e errorResp
		err := json.NewDecoder(res.Body).Decode(&e)
		if err != nil {
			return errors.New("Unexpected error: " + res.Status)
		}

		return errors.New(e.Message)
	}

	return nil
}
