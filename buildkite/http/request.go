package http

import (
	"fmt"
	"time"
)

type Request struct {
	Session     Session // A session to inherit defaults from
	Endpoint    string  // Endpoint can include a path, i.e. https://agent.buildkite.com/v2
	Path        string
	Method      string
	Headers     []Header
	Body        interface{}
	Timeout     time.Duration
	ContentType string
	Accept      string
	UserAgent   string
	ShowDebug   bool
}

func (r *Request) String() string {
	return fmt.Sprintf("http.Request{Method: %s, URL: %s}", r.Method, r.URL())
}

func (r *Request) AddHeader(name string, value string) {
	if r.Headers == nil {
		r.Headers = []Header{}
	}

	r.Headers = append(r.Headers, Header{Name: name, Value: value})
}

func (r *Request) Do() {
}

func (r *Request) URL() string {
	return r.Endpoint + r.Path
}
