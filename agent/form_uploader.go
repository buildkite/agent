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

type FormUploader struct {
	// Whether or not HTTP calls shoud be debugged
	DebugHTTP bool
}

func (u *FormUploader) Setup(destination string, debugHTTP bool) error {
	u.DebugHTTP = debugHTTP

	return nil
}

// The FormUploader doens't specify a URL, as one is provided by Buildkite
// after uploading
func (u *FormUploader) URL(artifact *api.Artifact) string {
	return ""
}

func (u *FormUploader) Upload(artifact *api.Artifact) error {
	// Create a HTTP request for uploading the file
	request, err := createUploadRequest(artifact)
	if err != nil {
		return err
	}

	// Create the client
	client := &http.Client{}

	// Perform the request
	logger.Debug("%s %s", request.Method, request.URL)
	response, err := client.Do(request)

	// Check for errors
	if err != nil {
		return err
	} else {
		// Be sure to close the response body at the end of
		// this function
		defer response.Body.Close()

		if u.DebugHTTP {
			responseDump, err := httputil.DumpResponse(response, true)
			logger.Debug("\nERR: %s\n%s", err, string(responseDump))
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
func createUploadRequest(artifact *api.Artifact) (*http.Request, error) {
	file, err := os.Open(artifact.AbsolutePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Set the post data for the request
	for key, val := range artifact.UploadInstructions.Data {
		// Replace the magical ${artifact:path} variable with the
		// artifact's path
		newVal := ArtifactPathVariableRegex.ReplaceAllLiteralString(val, artifact.Path)

		// Write the new value to the form
		err = writer.WriteField(key, newVal)
		if err != nil {
			return nil, err
		}
	}

	// It's important that we add the form field last because when
	// uploading to an S3 form, they are really nit-picky about the field
	// order, and the file needs to be the last one other it doesn't work.
	part, err := writer.CreateFormFile(artifact.UploadInstructions.Action.FileInput, artifact.Path)
	if err != nil {
		return nil, err
	}

	_, err = io.Copy(part, file)
	if err != nil {
		return nil, err
	}

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	// Create the URL that we'll send data to
	uri, err := url.Parse(artifact.UploadInstructions.Action.URL)
	if err != nil {
		return nil, err
	}

	uri.Path = artifact.UploadInstructions.Action.Path

	// Create the request
	req, err := http.NewRequest(artifact.UploadInstructions.Action.Method, uri.String(), body)
	if err != nil {
		return nil, err
	}

	// Finally add the multipart content type to the request
	req.Header.Add("Content-Type", writer.FormDataContentType())

	return req, nil
}
