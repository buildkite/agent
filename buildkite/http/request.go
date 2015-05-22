package http

import (
	"fmt"
	"github.com/buildkite/agent/buildkite/logger"
	stdhttp "net/http"
	// stdhttputil "net/http/httputil"
	"bytes"
	"io"
	"net"
	"strings"
	"time"
)

type body interface {
	ToBody() (*bytes.Buffer, error)
	ContentType() string
}

type Request struct {
	Session       *Session // A session to inherit defaults from
	Endpoint      string   // Endpoint can include a path, i.e. https://agent.buildkite.com/v2
	Path          string
	Method        string
	Headers       []Header
	Body          body
	ContentType   string
	Accept        string
	UserAgent     string
	Timeout       time.Duration
	Retries       int
	RetryCallback func(*Response) bool
}

func (r *Request) String() string {
	return fmt.Sprintf("http.Request{Method: %s, URL: %s}", r.Method, r.URL())
}

func (r *Request) Copy() Request {
	return Request{
		Session:       r.Session,
		Endpoint:      r.Endpoint,
		Path:          r.Path,
		Method:        r.Method,
		Headers:       r.Headers,
		Body:          r.Body,
		ContentType:   r.ContentType,
		Accept:        r.Accept,
		UserAgent:     r.UserAgent,
		Timeout:       r.Timeout,
		Retries:       r.Retries,
		RetryCallback: r.RetryCallback,
	}
}

func (r *Request) AddHeader(name string, value string) {
	if r.Headers == nil {
		r.Headers = []Header{}
	}

	r.Headers = append(r.Headers, Header{Name: name, Value: value})
}

func (r *Request) URL() string {
	if r.Session != nil && r.Session.Endpoint != "" {
		return r.join(r.Session.Endpoint, r.Path)
	} else if r.Endpoint != "" {
		return r.join(r.Endpoint, r.Path)
	} else {
		return r.Path
	}
}

func (r *Request) Do() (*Response, error) {
	var response *Response
	var err error

	seconds := 5 * time.Second
	ticker := time.NewTicker(seconds)
	retries := 1

	// The retires value can't be less than 0
	max := r.Retries
	if max <= 0 {
		max = 1
	}

	for {
		// Only show the retries in the log if we're on our second+
		// attempt
		if retries > 1 {
			logger.Debug("%s %s (Attempt %d/%d)", r.Method, r.URL(), retries, max)
		} else {
			logger.Debug("%s %s", r.Method, r.URL())
		}

		response, err = r.send()

		if err == nil {
			break
		}

		if retries >= max {
			logger.Warn("%s %s (%d/%d) (%T: %v)", r.Method, r.URL(), retries, max, err, err)
			break
		} else {
			if r.RetryCallback != nil {
				// If the RetryCallback returns false, don't
				// bother retrying
				if r.RetryCallback(response) == false {
					break
				}
			}

			logger.Warn("%s %s (%d/%d) (%T: %v) Trying again in %s", r.Method, r.URL(), retries, max, err, err, seconds)
		}

		// We don't return this response, so we should make sure we close it's body
		if response != nil && response.Body != nil {
			response.Body.Close()
		}

		retries++
		<-ticker.C
	}

	return response, err
}

func (r *Request) send() (*Response, error) {
	var body io.Reader
	var err error

	if r.Body != nil {
		body, err = r.Body.ToBody()
		if err != nil {
			return nil, err
		}
	}

	req, err := stdhttp.NewRequest(r.Method, r.URL(), body)
	if err != nil {
		return nil, err
	}

	// Add the content type of the body if there is one
	if r.Body != nil {
		req.Header.Set("Content-Type", r.Body.ContentType())
	}

	// Add in the headers
	for _, header := range r.Headers {
		req.Header.Set(header.Name, header.Value)
	}

	// Add in the sessions headers (if we have a session)
	if r.Session != nil {
		for _, header := range r.Session.Headers {
			req.Header.Set(header.Name, header.Value)
		}
	}

	// debug, _ := stdhttputil.DumpRequest(req, true)
	// logger.Debug("%s", debug)

	// Construct a new dialer and bump the default timeout
	dialer := &net.Dialer{
		Timeout: 60 * time.Second,
	}

	// Set the custom timeout
	if r.Timeout > 0 {
		dialer.Timeout = r.Timeout
	}

	// New transport and bump the TLSHandshakeTimeout
	transport := &stdhttp.Transport{
		Dial:                dialer.Dial,
		Proxy:               stdhttp.ProxyFromEnvironment,
		TLSHandshakeTimeout: 60 * time.Second,
	}

	client := &stdhttp.Client{Transport: transport}

	// Perform the stdhttp request
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	response := &Response{
		StatusCode: res.StatusCode,
		Body:       &Body{reader: res.Body},
	}

	// Was the request not a: 200, 201, 202, etc
	if res.StatusCode/100 != 2 {
		return response, Error{Status: res.Status}
	}

	return response, nil
}

func (r *Request) join(endpoint string, path string) string {
	return strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(path, "/")
}
