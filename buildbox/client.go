package buildbox

import (
  "net/http"
  _ "crypto/sha512" // import sha512 to make sha512 ssl certs work
  "encoding/json"
  "runtime"
  "strings"
  "io"
  "errors"
  "bytes"
)

const (
  DefaultAPIURL = "https://agent.buildbox.io/v1"
  DefaultUserAgent = "buildbox-agent/" + Version + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
)

type Client struct {
  // The URL of the Buildbox Agent API to communicate with. Defaults to
  // "https://agent.buildbox.io/v1".
  URL string

  // The access token of the agent being used to make API requests
  AgentAccessToken string

  // UserAgent to be provided in API requests. Set to DefaultUserAgent if not
  // specified.
  UserAgent string
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

// Sends a Buildbox API request and decodes the response into v.
func (c *Client) APIReq(v interface{}, method string, path string, body interface{}) error {
  // Generate a new request
  req, err := c.NewRequest(method, path, body)
  if err != nil {
    return err
  }

  // Perform the request
  return c.DoReq(req, v)
}

// Generates an HTTP request for the Buildbox API, but does not
// perform the request.
func (c *Client) NewRequest(method string, path string, body interface{}) (*http.Request, error) {
  // Popualte the request body if we have to
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
    endpointUrl = DefaultAPIURL
  }

  normalizedPath := strings.TrimLeft(path, "/")
  url := endpointUrl +"/" + c.AgentAccessToken + "/" + normalizedPath

  // Create a new request object
  req, err := http.NewRequest(method, url, requestBody)
  if err != nil {
    return nil, err
  }

  // Set the accept content type. The Buildbox API only speaks
  // json.
  req.Header.Set("Accept", "application/json")

  // Figure out and set the User Agent
  userAgent := c.UserAgent
  if userAgent == "" {
    userAgent = DefaultUserAgent
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
  Logger.Debugf("%s %s", req.Method, req.URL)

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

  // Decode the response
  return json.NewDecoder(res.Body).Decode(v)
}

type errorResp struct {
  Message string `json:"error"`
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
