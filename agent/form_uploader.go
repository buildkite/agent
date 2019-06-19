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

	streamer := newMultipartStreamer()

	// Set the post data for the request
	for key, val := range artifact.UploadInstructions.Data {
		// Replace the magical ${artifact:path} variable with the
		// artifact's path
		newVal := ArtifactPathVariableRegex.ReplaceAllLiteralString(val, artifact.Path)

		// Write the new value to the form
		err = streamer.WriteField(key, newVal)
		if err != nil {
			return nil, err
		}
	}

	// It's important that we add the form field last because when
	// uploading to an S3 form, they are really nit-picky about the field
	// order, and the file needs to be the last one other it doesn't work.
	if err := streamer.WriteFile(artifact.UploadInstructions.Action.FileInput, artifact.Path); err != nil {
		return nil, err
	}

	// Create the URL that we'll send data to
	uri, err := url.Parse(artifact.UploadInstructions.Action.URL)
	if err != nil {
		return nil, err
	}

	uri.Path = artifact.UploadInstructions.Action.Path

	// Create the request
	req, err := http.NewRequest(artifact.UploadInstructions.Action.Method, uri.String(), streamer.Reader())
	if err != nil {
		return nil, err
	}

	// Setup the content type and length that s3 requires
	req.Header.Add("Content-Type", streamer.ContentType)
	req.ContentLength = streamer.Len()

	return req, nil
}

// A wrapper around the complexities of streaming a multipart file and fields to
// an http endpoint that infuriatingly requires a Content-Length
// Derived from https://github.com/technoweenie/multipartstreamer
type multipartStreamer struct {
	ContentType   string
	bodyBuffer    *bytes.Buffer
	bodyWriter    *multipart.Writer
	closeBuffer   *bytes.Buffer
	reader        io.ReadCloser
	contentLength int64
}

// newMultipartStreamer initializes a new MultipartStreamer.
func newMultipartStreamer() *multipartStreamer {
	m := &multipartStreamer{
		bodyBuffer: new(bytes.Buffer),
	}

	m.bodyWriter = multipart.NewWriter(m.bodyBuffer)
	boundary := m.bodyWriter.Boundary()
	m.ContentType = "multipart/form-data; boundary=" + boundary

	closeBoundary := fmt.Sprintf("\r\n--%s--\r\n", boundary)
	m.closeBuffer = bytes.NewBufferString(closeBoundary)

	return m
}

// WriteField writes a form field to the multipart.Writer.
func (m *multipartStreamer) WriteField(key, value string) error {
	return m.bodyWriter.WriteField(key, value)
}

// WriteFile writes the multi-part preamble which will be followed by file data
// This can only be called once and must be the last thing written to the streamer
func (m *multipartStreamer) WriteFile(key, filename string) error {
	if m.reader != nil {
		return errors.New("WriteFile can't be called multiple times")
	}

	fh, err := os.Open(filename)
	if err != nil {
		return err
	}

	stat, err := fh.Stat()
	if err != nil {
		return err
	}

	// Set up a reader that combines the body, the file and the closer in a stream
	m.reader = &multipartReadCloser{
		Reader: io.MultiReader(m.bodyBuffer, fh, m.closeBuffer),
		fh:     fh,
	}
	m.contentLength = stat.Size()

	_, err = m.bodyWriter.CreateFormFile(key, filename)
	return err
}

// Len calculates the byte size of the multipart content.
func (m *multipartStreamer) Len() int64 {
	return m.contentLength + int64(m.bodyBuffer.Len()) + int64(m.closeBuffer.Len())
}

// Reader gets an io.ReadCloser for passing to an http.Request.
func (m *multipartStreamer) Reader() io.ReadCloser {
	return m.reader
}

type multipartReadCloser struct {
	io.Reader
	fh *os.File
}

func (mrc *multipartReadCloser) Close() error {
	return mrc.fh.Close()
}
