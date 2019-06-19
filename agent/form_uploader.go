package agent

import (
	"bytes"
	_ "crypto/sha512" // import sha512 to make sha512 ssl certs work
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"regexp"

	// "net/http/httputil"
	"errors"
	"net/url"
	"os"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
)

var ArtifactPathVariableRegex = regexp.MustCompile("\\$\\{artifact\\:path\\}")

type FormUploaderConfig struct {
	// Whether or not HTTP calls should be debugged
	DebugHTTP bool
}

type FormUploader struct {
	// The configuration
	conf FormUploaderConfig

	// The logger instance to use
	logger logger.Logger
}

func NewFormUploader(l logger.Logger, c FormUploaderConfig) *FormUploader {
	return &FormUploader{
		logger: l,
		conf:   c,
	}
}

// The FormUploader doens't specify a URL, as one is provided by Buildkite
// after uploading
func (u *FormUploader) URL(artifact *api.Artifact) string {
	return ""
}

func (u *FormUploader) Upload(artifact *api.Artifact) error {
	// Create a HTTP request for uploading the file
	request, err := createUploadRequest(u.logger, artifact)
	if err != nil {
		return err
	}

	// Create the client
	client := &http.Client{}

	// Perform the request
	u.logger.Debug("%s %s", request.Method, request.URL)
	response, err := client.Do(request)

	// Check for errors
	if err != nil {
		return err
	} else {
		// Be sure to close the response body at the end of
		// this function
		defer response.Body.Close()

		if u.conf.DebugHTTP {
			responseDump, err := httputil.DumpResponse(response, true)
			u.logger.Debug("\nERR: %s\n%s", err, string(responseDump))
		}

		if response.StatusCode/100 != 2 {
			body := &bytes.Buffer{}
			_, err := body.ReadFrom(response.Body)
			if err != nil {
				return err
			}

			// Return a custom error with the response body from the page
			message := fmt.Sprintf("%s (%d)", body, response.StatusCode)
			return errors.New(message)
		}
	}

	return nil
}

// Creates a new file upload http request with optional extra params
func createUploadRequest(l logger.Logger, artifact *api.Artifact) (*http.Request, error) {
	// Check the file exists
	_, err := os.Stat(artifact.AbsolutePath)
	if err != nil {
		return nil, err
	}

	// Use a pipe for the body to avoid buffering the entire file in memory
	// See https://blog.depado.eu/post/bufferless-multipart-post-in-go
	pipeR, pipeW := io.Pipe()
	writer := multipart.NewWriter(pipeW)
	errCh := make(chan error)

	go func() {
		defer pipeW.Close()
		defer close(errCh)

		// Set the post data for the request
		for key, val := range artifact.UploadInstructions.Data {
			// Replace the magical ${artifact:path} variable with the
			// artifact's path
			newVal := ArtifactPathVariableRegex.ReplaceAllLiteralString(val, artifact.Path)

			// Write the new value to the form
			err = writer.WriteField(key, newVal)
			if err != nil {
				errCh <- err
				return
			}
		}
		// It's important that we add the form field last because when
		// uploading to an S3 form, they are really nit-picky about the field
		// order, and the file needs to be the last one other it doesn't work.
		part, err := writer.CreateFormFile(artifact.UploadInstructions.Action.FileInput, artifact.Path)
		if err != nil {
			return
		}

		file, err := os.Open(artifact.AbsolutePath)
		if err != nil {
			errCh <- err
			return
		}
		defer file.Close()

		if _, err = io.Copy(part, file); err != nil {
			errCh <- err
			return
		}

		if err := writer.Close(); err != nil {
			errCh <- err
			return
		}
	}()

	// Create the URL that we'll send data to
	uri, err := url.Parse(artifact.UploadInstructions.Action.URL)
	if err != nil {
		return nil, err
	}

	uri.Path = artifact.UploadInstructions.Action.Path

	// Create the request
	req, err := http.NewRequest(artifact.UploadInstructions.Action.Method, uri.String(), pipeR)
	if err != nil {
		return nil, err
	}

	// Finally add the multipart content type to the request
	req.Header.Add("Content-Type", writer.FormDataContentType())

	return req, nil
}
