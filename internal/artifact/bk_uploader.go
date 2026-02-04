package artifact

import (
	"bytes"
	"cmp"
	"context"
	_ "crypto/sha512" // import sha512 to make sha512 ssl certs work
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/agenthttp"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/version"
	"github.com/dustin/go-humanize"
)

const artifactPathVariable = "${artifact:path}"

const (
	// BKUploader uploads to S3 either as:
	// - a single signed POST, which has a hard limit of 5GB, or
	// - as a signed multipart, which has a limit of 5GB per _part_, but we
	//   aren't supporting larger artifacts yet.
	// Note that multipart parts have a minimum size of 5MB.
	maxFormUploadedArtifactSize = int64(5 * 1024 * 1024 * 1024)
)

type BKUploaderConfig struct {
	// Standard HTTP options
	DebugHTTP    bool
	TraceHTTP    bool
	DisableHTTP2 bool
}

// BKUploader uploads artifacts to Buildkite itself.
type BKUploader struct {
	// The configuration
	conf BKUploaderConfig

	// The logger instance to use
	logger logger.Logger
}

// NewBKUploader creates a new Buildkite uploader.
func NewBKUploader(l logger.Logger, c BKUploaderConfig) *BKUploader {
	return &BKUploader{
		logger: l,
		conf:   c,
	}
}

// URL returns the empty string. BKUploader doesn't know the URL in advance,
// it is provided by Buildkite after uploading.
func (u *BKUploader) URL(*api.Artifact) string { return "" }

// CreateWork checks the artifact size, then creates one worker.
func (u *BKUploader) CreateWork(artifact *api.Artifact) ([]workUnit, error) {
	if artifact.FileSize > maxFormUploadedArtifactSize {
		return nil, errArtifactTooLarge{Size: artifact.FileSize}
	}
	actions := artifact.UploadInstructions.Actions
	if len(actions) == 0 {
		// Not multiple actions - use a single form upload.
		return []workUnit{&bkFormUpload{
			BKUploader: u,
			artifact:   artifact,
		}}, nil
	}

	// Ensure the actions are sorted by part number.
	slices.SortFunc(actions, func(a, b api.ArtifactUploadAction) int {
		return cmp.Compare(a.PartNumber, b.PartNumber)
	})

	// Split the artifact across multiple parts.
	chunks := int64(len(actions))
	chunkSize := artifact.FileSize / chunks
	remainder := artifact.FileSize % chunks
	var offset int64
	workUnits := make([]workUnit, 0, chunks)
	for i, action := range actions {
		size := chunkSize
		if int64(i) < remainder {
			// Spread the remainder across the first chunks.
			size++
		}
		workUnits = append(workUnits, &bkMultipartUpload{
			BKUploader: u,
			artifact:   artifact,
			partCount:  int(chunks),
			action:     &action,
			offset:     offset,
			size:       size,
		})
		offset += size
	}
	// After that loop, `offset` should equal `artifact.FileSize`.
	return workUnits, nil
}

// bkMultipartUpload uploads a single part of a multipart upload.
type bkMultipartUpload struct {
	*BKUploader
	artifact     *api.Artifact
	action       *api.ArtifactUploadAction
	partCount    int
	offset, size int64
}

func (u *bkMultipartUpload) Artifact() *api.Artifact { return u.artifact }

func (u *bkMultipartUpload) Description() string {
	return fmt.Sprintf("%s %s part %d/%d (~%s starting at ~%s)",
		u.artifact.ID,
		u.artifact.Path,
		u.action.PartNumber,
		u.partCount,
		humanize.IBytes(uint64(u.size)),
		humanize.IBytes(uint64(u.offset)),
	)
}

func (u *bkMultipartUpload) DoWork(ctx context.Context) (*api.ArtifactPartETag, error) {
	f, err := os.Open(u.artifact.AbsolutePath)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // File open for read only.

	if _, err := f.Seek(u.offset, 0); err != nil {
		return nil, fmt.Errorf("seeking input file to offset %d: %w", u.offset, err)
	}
	lr := io.LimitReader(f, u.size)

	req, err := http.NewRequestWithContext(ctx, u.action.Method, u.action.URL, lr)
	if err != nil {
		return nil, err
	}

	// Content-Range would be useful for debugging purposes, but S3 will reject
	// the request.
	// Content-Length is needed to avoid Go adding Transfer-Encoding: chunked
	// which would also cause S3 to reject the request (plus we know the part
	// length in advance).
	req.ContentLength = u.size
	req.Header.Set("Content-Type", u.artifact.ContentType)

	client := agenthttp.NewClient(
		agenthttp.WithAllowHTTP2(!u.conf.DisableHTTP2),
		agenthttp.WithNoTimeout,
	)

	resp, err := agenthttp.Do(u.logger, client, req,
		agenthttp.WithDebugHTTP(u.conf.DebugHTTP),
		agenthttp.WithTraceHTTP(u.conf.TraceHTTP),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // Idiomatic for response bodies.

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %v", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("unsuccessful status %s: %s", resp.Status, body)
	}

	etag := resp.Header.Get("Etag")
	u.logger.Debug("Artifact %s part %d has ETag = %s", u.artifact.ID, u.action.PartNumber, etag)
	if etag == "" {
		return nil, errors.New("response missing ETag header")
	}

	return &api.ArtifactPartETag{
		PartNumber: u.action.PartNumber,
		ETag:       etag,
	}, nil
}

// bkFormUpload uploads an artifact to a presigned URL in a single request using
// a request body encoded as multipart/form-data.
type bkFormUpload struct {
	*BKUploader
	artifact *api.Artifact
}

func (u *bkFormUpload) Artifact() *api.Artifact { return u.artifact }

func (u *bkFormUpload) Description() string {
	return singleUnitDescription(u.artifact)
}

// DoWork tries the upload.
func (u *bkFormUpload) DoWork(ctx context.Context) (*api.ArtifactPartETag, error) {
	request, err := createFormUploadRequest(ctx, u.logger, u.artifact)
	if err != nil {
		return nil, err
	}

	// Create the client
	client := agenthttp.NewClient(
		agenthttp.WithAllowHTTP2(!u.conf.DisableHTTP2),
		agenthttp.WithNoTimeout,
	)

	// Perform the request
	response, err := agenthttp.Do(u.logger, client, request,
		agenthttp.WithDebugHTTP(u.conf.DebugHTTP),
		agenthttp.WithTraceHTTP(u.conf.TraceHTTP),
	)
	if err != nil {
		return nil, err
	}

	// Be sure to close the response body at the end of
	// this function
	defer response.Body.Close() //nolint:errcheck // Idiomatic for response bodies.

	if response.StatusCode/100 != 2 {
		body := &bytes.Buffer{}
		_, err := body.ReadFrom(response.Body)
		if err != nil {
			return nil, err
		}

		// Return a custom error with the response body from the page
		return nil, fmt.Errorf("%s (%d)", body, response.StatusCode)
	}
	return nil, nil
}

// Creates a new file upload http request with optional extra params
func createFormUploadRequest(ctx context.Context, _ logger.Logger, artifact *api.Artifact) (_ *http.Request, err error) {
	streamer := newMultipartStreamer()
	action := artifact.UploadInstructions.Action

	// Set the post data for the request
	for key, val := range artifact.UploadInstructions.Data {
		// Replace the magical ${artifact:path} variable with the
		// artifact's path
		newVal := strings.ReplaceAll(val, artifactPathVariable, artifact.Path)

		// Write the new value to the form
		if err := streamer.WriteField(key, newVal); err != nil {
			return nil, err
		}
	}

	fh, err := os.Open(artifact.AbsolutePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		// In the happy case, don't close the file - let the caller do that.
		// In the sad case, close it before returning.
		// This uses the named error return for the function to read the error.
		if err != nil {
			fh.Close() //nolint:errcheck // File open for read only.
		}
	}()

	// It's important that we add the form field last because when
	// uploading to an S3 form, they are really nit-picky about the field
	// order, and the file needs to be the last one other it doesn't work.
	if err := streamer.WriteFile(action.FileInput, artifact.Path, fh); err != nil {
		return nil, err
	}

	// Create the URL that we'll send data to
	uri, err := url.Parse(action.URL)
	if err != nil {
		return nil, err
	}

	uri.Path = artifact.UploadInstructions.Action.Path

	// Create the request
	req, err := http.NewRequestWithContext(ctx, action.Method, uri.String(), streamer.Reader())
	if err != nil {
		return nil, err
	}

	// Setup the content type and length that s3 requires
	req.Header.Add("Content-Type", streamer.ContentType)
	// Letting the server know the agent version can be helpful for debugging
	req.Header.Add("User-Agent", version.UserAgent())
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
func (m *multipartStreamer) WriteFile(key, artifactPath string, fh http.File) error {
	if m.reader != nil {
		return errors.New("WriteFile can't be called multiple times")
	}

	// Set up a reader that combines the body, the file and the closer in a stream
	m.reader = &multipartReadCloser{
		Reader: io.MultiReader(m.bodyBuffer, fh, m.closeBuffer),
		fh:     fh,
	}

	stat, err := fh.Stat()
	if err != nil {
		return err
	}

	m.contentLength = stat.Size()

	_, err = m.bodyWriter.CreateFormFile(key, artifactPath)
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
	fh http.File
}

func (mrc *multipartReadCloser) Close() error {
	return mrc.fh.Close()
}

type errArtifactTooLarge struct {
	Size int64
}

func (e errArtifactTooLarge) Error() string {
	// TODO: Clean up error strings
	// https://github.com/golang/go/wiki/CodeReviewComments#error-strings
	return fmt.Sprintf("File size (%d bytes) exceeds the maximum supported by Buildkite's default artifact storage (5Gb). Alternative artifact storage options may support larger files.", e.Size)
}
