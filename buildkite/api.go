package buildkite

import (
	"github.com/buildkite/agent/buildkite/http"
	"github.com/buildkite/agent/buildkite/logger"
	"runtime"
)

type API struct {
	// The Endpoint of the Buildkite Agent API to communicate with. Defaults to
	// "https://agent.buildkite.com/v2".
	Endpoint string

	// The authorization token agent being used to make API requests
	Token string
}

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

func (api API) Do(method string, path string, result interface{}, body interface{}, _retries ...int) error {
	retries := 10
	if len(_retries) > 0 {
		retries = _retries[0]
	}

	// We can't use a pre-existing http.Session here, because registration
	// uses a different authorization token
	request := http.Request{
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
		RetryCallback: func(response *http.Response) {
			if response.Body != nil {
				// Ensure the response body is closed after
				// this callback
				defer response.Body.Close()

				api.logError(response)
			}
		},
	}

	// Add the body if it's present
	if body != nil {
		request.Body = &http.JSON{Payload: body}
	}

	// Perform the request
	response, err := request.Do()

	// If a body was returned, make sure we close it at the end of this function
	if response.Body != nil {
		defer response.Body.Close()
	}

	// Bail if there was an error
	if err != nil {
		if response.Body != nil {
			api.logError(response)
		}

		return err
	}

	// Copy the JSON response to our agent record
	return response.Body.DecodeFromJSON(&result)
}

func (api API) logError(response *http.Response) {
	// See if there was an error embedded in the JSON that we should show
	var err APIErrorResponse
	response.Body.DecodeFromJSON(&err)

	if err.Message != "" {
		logger.Warn("%s", err.Message)
	}
}
