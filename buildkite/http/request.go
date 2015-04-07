package http

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/cenkalti/backoff"
	"mime/multipart"
	stdhttp "net/http"
	"reflect"
	"time"
)

type Request struct {
	Session         *Session // A session to inherit defaults from
	Endpoint        string   // Endpoint can include a path, i.e. https://agent.buildkite.com/v2
	Path            string
	Method          string
	Headers         []Header
	Params          map[string]interface{}
	ContentType     string
	Accept          string
	UserAgent       string
	Timeout         time.Duration
	MaxElapsedTime  time.Duration
	MaxIntervalTime time.Duration
}

func NewRequest(method string, path string) Request {
	return Request{
		Method: method,
		Path:   path,
		Params: map[string]interface{}{},
	}
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

func (r *Request) Do() (*Response, error) {
	logger.Debug("Performing request: %s", r.URL())

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

	// The retryable portion of this function
	retryable := func() error {
		logger.Debug("%s %s", r.Method, r.URL())

		// Perform the stdhttp request
		res, err := stdhttp.DefaultClient.Do(req)
		if err != nil {
			return err
		}

		// Be sure to close the response body at the end of
		// this function
		defer res.Body.Close()

		// Was the request not a: 200, 201, 202, etc
		if res.StatusCode/100 != 2 {
			return errors.New("Unexpected error: " + res.Status)
		} else {
			response.StatusCode = res.StatusCode
		}

		return nil
	}

	// Is called when ever there is an error
	notify := func(err error, wait time.Duration) {
		logger.Error("Failed to %s to %s with error \"%s\". Will try again in %s", r.Method, r.URL(), err, wait)
	}

	exponentialBackOff := backoff.NewExponentialBackOff()
	exponentialBackOff.MaxElapsedTime = r.MaxElapsedTime
	exponentialBackOff.MaxInterval = r.MaxIntervalTime

	err = backoff.RetryNotify(retryable, exponentialBackOff, notify)
	if err != nil {
		// Operation has finally failed and will not be retried
		return nil, err
	}

	return response, nil
}

func (r *Request) URL() string {
	if r.Session != nil && r.Session.Endpoint != "" {
		return r.Session.Endpoint + r.Path
	} else {
		return r.Endpoint + r.Path
	}
}
