package buildkite

import (
	"github.com/buildkite/agent/http"
	"github.com/buildkite/agent/logger"
	"runtime"
)

type API struct {
	// The Endpoint of the Buildkite Agent API to communicate with. Defaults to
	// "https://agent.buildkite.com/v2".
	Endpoint string

	// The authorization token agent being used to make API requests
	Token string
}

const (
	// Passing this constant to retries will tell the API gear that it
	// should keep retrying the call until it succeeds.
	APIInfinityRetires = http.RetryForever
)

type APIErrorResponse struct {
	Message string `json:"message"`
}

func (api API) UserAgent() string {
	return "buildkite-agent/" + Version() + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
}

func (api API) Get(path string, result interface{}, _retries ...int) error {
	return api.Do("GET", path, result, nil, _retries...)
}

func (api API) Post(path string, result interface{}, body interface{}, _retries ...int) error {
	return api.Do("POST", path, result, body, _retries...)
}

func (api API) Put(path string, result interface{}, body interface{}, _retries ...int) error {
	return api.Do("PUT", path, result, body, _retries...)
}

func (api API) NewRequest(method string, path string, retries int) http.Request {
	return http.Request{
		Endpoint: api.Endpoint,
		Method:   method,
		Path:     path,
		Headers: []http.Header{
			http.Header{
				Name:  "Authorization",
				Value: "Token " + api.Token,
			},
		},
		UserAgent: api.UserAgent(),
		Retries:   retries,
		RetryCallback: func(response *http.Response) bool {
			if response != nil {
				// Try an log an error
				if response.Body != nil {
					api.logError(response)
				}

				// Don't bother retrying these statuses
				if response.StatusCode == 401 || response.StatusCode == 404 {
					return false
				}

				// Returning true will cause it to retry as usuaul
				return true
			} else {
				// No response object at all? Something must have gone very wrong.
				return false
			}
		},
	}
}

func (api API) Do(method string, path string, result interface{}, body interface{}, _retries ...int) error {
	retries := 10
	if len(_retries) > 0 {
		retries = _retries[0]
	}

	// We can't use a pre-existing http.Session here, because registration
	// uses a different authorization token
	request := api.NewRequest(method, path, retries)

	// Add the body if it's present
	if body != nil {
		request.Body = &http.JSON{Payload: body}
	}

	// Perform the request
	response, err := request.Do()

	// Bail if there was an error
	if err != nil {
		if response != nil && response.Body != nil {
			api.logError(response)
		}

		return err
	}

	// After decoding from JSON, ensure the response has been closed
	defer response.Body.Close()

	// Copy the JSON response to our agent record
	return response.Body.DecodeFromJSON(&result)
}

func (api API) logError(response *http.Response) {
	// Ensure the response body is closed after showing the error
	defer response.Body.Close()

	// See if there was an error embedded in the JSON that we should show
	var err APIErrorResponse
	response.Body.DecodeFromJSON(&err)

	if err.Message != "" {
		logger.Warn("%s", err.Message)
	}
}
