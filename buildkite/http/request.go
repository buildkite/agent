package http

import (
	"bytes"
	"fmt"
	"github.com/buildkite/agent/buildkite/logger"
	"mime/multipart"
	stdhttp "net/http"
	"reflect"
	"time"
)

type Request struct {
	Session     *Session // A session to inherit defaults from
	Endpoint    string   // Endpoint can include a path, i.e. https://agent.buildkite.com/v2
	Path        string
	Method      string
	Headers     []Header
	Params      map[string]interface{}
	ContentType string
	Accept      string
	UserAgent   string
	Timeout     time.Duration
	Retries     int
}

func NewRequest(method string, path string) Request {
	return Request{
		Method:  method,
		Path:    path,
		Params:  map[string]interface{}{},
		Retries: 1,
	}
}

func (r *Request) String() string {
	return fmt.Sprintf("http.Request{Method: %s, URL: %s}", r.Method, r.URL())
}

func (r *Request) Copy() Request {
	return Request{
		Session:     r.Session,
		Endpoint:    r.Endpoint,
		Path:        r.Path,
		Method:      r.Method,
		Headers:     r.Headers,
		Params:      r.Params,
		ContentType: r.ContentType,
		Accept:      r.Accept,
		UserAgent:   r.UserAgent,
		Timeout:     r.Timeout,
		Retries:     r.Retries,
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
		return r.Session.Endpoint + r.Path
	} else {
		return r.Endpoint + r.Path
	}
}

func (r *Request) Do() (*Response, error) {
	var response *Response
	var err error

	seconds := 5 * time.Second
	ticker := time.NewTicker(seconds)
	retries := 1

	for {
		logger.Debug("%s %s (%d/%d)", r.Method, r.URL(), retries, r.Retries)

		response, err = r.send()
		if err == nil {
			break
		}

		if retries >= r.Retries {
			logger.Warn("%s %s (%d/%d) (%T: %v)", r.Method, r.URL(), retries, r.Retries, err, err)
			break
		} else {
			logger.Warn("%s %s (%d/%d) (%T: %v) Trying again in %s", r.Method, r.URL(), retries, r.Retries, err, err, seconds)
		}

		retries++
		<-ticker.C
	}

	return response, err
}

func (r *Request) send() (*Response, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for name, value := range r.Params {
		typeOf := reflect.TypeOf(value).String()

		if typeOf == "http.MultiPart" {
			multiPart, _ := value.(MultiPart)

			part, err := writer.CreateFormFile(name, multiPart.FileName)
			if err != nil {
				return nil, err
			}

			part.Write([]byte(multiPart.Data))
		} else {
			_ = writer.WriteField(name, fmt.Sprintf("%s", value))
		}
	}

	err := writer.Close()
	if err != nil {
		return nil, err
	}

	req, err := stdhttp.NewRequest(r.Method, r.URL(), body)
	if err != nil {
		return nil, err
	}

	response := new(Response)

	// Perform the stdhttp request
	res, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	// Be sure to close the response body at the end of this function
	defer res.Body.Close()

	// Was the request not a: 200, 201, 202, etc
	if res.StatusCode/100 != 2 {
		return nil, Error{Status: res.Status}
	} else {
		response.StatusCode = res.StatusCode
	}

	return response, nil
}
