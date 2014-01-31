package buildbox

// Inspired from:
// https://github.com/bgentry/heroku-go/blob/ab3320c0b603292a42f39cd41a0a8f71d1b3716b/heroku.go

import (
  "log"
  "net/http"
  "encoding/json"
  "runtime"
)

const (
  Version          = "0.1"
  DefaultAPIURL    = "https://agent.buildbox.io/v1"
  DefaultUserAgent = "heroku-go/" + Version + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
)

type Client struct {
  // The URL of the Buildbox Agent API to communicate with. Defaults to
  // "https://agent.buildbox.io/v1".
  URL string

  // The access token of the agent being used to make API requests
  AgentAccessToken string

  // Debug mode can be used to dump the full request and response to stdout.
  Debug bool

  // UserAgent to be provided in API requests. Set to DefaultUserAgent if not
  // specified.
  UserAgent string
}

type Response struct {
  Build *Build
}

func (c *Client) Get(v interface{}, path string) error {
  return c.APIReq(v, "GET", path, nil)
}

func Get(url string) (*http.Response) {
  log.Printf("GET %s", url)

  var r *Response = new(Response)
  err := json.NewDecoder(resp.Body).Decode(r)
  if err != nil {
    log.Fatal(err)
  }

  return r.Build

  resp, err := http.Get(url)

  // Check to make sure no error returned from the get request
  if err != nil {
    log.Fatal(err)
  }

  // Check the status code
  if resp.StatusCode != http.StatusOK {
    log.Fatal(resp.Status)
  }

  return resp
}
