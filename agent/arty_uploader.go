package agent

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
)

type ArtifactoryUploaderConfig struct {
	// The destination which includes the Artifactory bucket name and the path.
	// e.g artifactory://my-repo-name/foo/bar
	Destination string

	// Whether or not HTTP calls should be debugged
	DebugHTTP bool
}

type ArtifactoryUploader struct {
	// The artifactory bucket path set from the destination
	Path string

	// The artifactory bucket name set from the destination
	Repository string

	// URL of artifactory instance
	iURL *url.URL

	// The artifactory client to use
	client *http.Client

	// The configuration
	conf ArtifactoryUploaderConfig

	// The logger instance to use
	logger logger.Logger

	// Artifactory username
	user string

	// Artifactory password
	password string
}

func NewArtifactoryUploader(l logger.Logger, c ArtifactoryUploaderConfig) (*ArtifactoryUploader, error) {
	repo, path := ParseArtifactoryDestination(c.Destination)
	stringURL := os.Getenv("BUILDKITE_ARTIFACTORY_URL")
	username := os.Getenv("BUILDKITE_ARTIFACTORY_USER")
	password := os.Getenv("BUILDKITE_ARTIFACTORY_PASSWORD")
	// authentication is not set
	if stringURL == "" || username == "" || password == "" {
		return nil, errors.New("Must set BUILDKITE_ARTIFACTORY_URL, BUILDKITE_ARTIFACTORY_USER, BUILDKITE_ARTIFACTORY_PASSWORD when using rt:// path")
	}

	parsedURL, err := url.Parse(stringURL)
	if err != nil {
		return nil, err
	}
	return &ArtifactoryUploader{
		logger:     l,
		conf:       c,
		client:     &http.Client{},
		iURL:       parsedURL,
		Path:       path,
		Repository: repo,
		user:       username,
		password:   password,
	}, nil
}

func ParseArtifactoryDestination(destination string) (repo string, path string) {
	parts := strings.Split(strings.TrimPrefix(string(destination), "rt://"), "/")
	path = strings.Join(parts[1:len(parts)], "/")
	repo = parts[0]
	return
}

func (u *ArtifactoryUploader) URL(artifact *api.Artifact) string {
	url := *u.iURL
	// ensure proper URL formatting for upload
	url.Path = path.Join(
		url.Path,
		filepath.ToSlash(u.artifactPath(artifact)),
	)
	return url.String()
}

func (u *ArtifactoryUploader) Upload(artifact *api.Artifact) error {
	// Open file from filesystem
	u.logger.Debug("Reading file \"%s\"", artifact.AbsolutePath)
	f, err := os.Open(artifact.AbsolutePath)
	if err != nil {
		return fmt.Errorf("failed to open file %q (%v)", artifact.AbsolutePath, err)
	}

	// Upload the file to Artifactory.
	u.logger.Debug("Uploading \"%s\" to `%s`", artifact.Path, u.URL(artifact))

	req, err := http.NewRequest("PUT", u.URL(artifact), f)
	req.SetBasicAuth(u.user, u.password)
	if err != nil {
		return err
	}

	md5Checksum, err := checksumFile(md5.New(), artifact.AbsolutePath)
	if err != nil {
		return err
	}
	req.Header.Add(`X-Checksum-MD5`, md5Checksum)

	sha1Checksum, err := checksumFile(sha1.New(), artifact.AbsolutePath)
	if err != nil {
		return err
	}
	req.Header.Add(`X-Checksum-SHA1`, sha1Checksum)

	sha256Checksum, err := checksumFile(sha256.New(), artifact.AbsolutePath)
	if err != nil {
		return err
	}
	req.Header.Add(`X-Checksum-SHA256`, sha256Checksum)

	res, err := u.client.Do(req)
	if err != nil {
		return err
	}
	if err := checkResponse(res); err != nil {
		return err
	}

	return nil
}

func checksumFile(hasher hash.Hash, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

func sha1File(path string) ([]byte, error) {
	hasher := sha1.New()

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := io.Copy(hasher, f); err != nil {
		return nil, err
	}

	return hasher.Sum(nil), nil
}

func (u *ArtifactoryUploader) artifactPath(artifact *api.Artifact) string {
	parts := []string{u.Repository, u.Path, artifact.Path}

	return strings.Join(parts, "/")
}

// An ErrorResponse reports one or more errors caused by an API request.
type errorResponse struct {
	Response *http.Response // HTTP response that caused this error
	Errors   []Error        `json:"errors"` // more detail on individual errors
}

func (r *errorResponse) Error() string {
	return fmt.Sprintf("%v %v: %d %+v",
		r.Response.Request.Method, r.Response.Request.URL,
		r.Response.StatusCode, r.Errors)
}

// An Error reports more details on an individual error in an ErrorResponse.
type Error struct {
	Status  int    `json:"status"`  // Error code
	Message string `json:"message"` // Message describing the error.
}

// checkResponse checks the API response for errors, and returns them if
// present. A response is considered an error if it has a status code outside
// the 200 range.
// API error responses are expected to have either no response
// body, or a JSON response body that maps to ErrorResponse. Any other
// response body will be silently ignored.
func checkResponse(r *http.Response) error {
	if c := r.StatusCode; 200 <= c && c <= 299 {
		return nil
	}
	errorResponse := &errorResponse{Response: r}
	data, err := ioutil.ReadAll(r.Body)
	if err == nil && data != nil {
		err := json.Unmarshal(data, errorResponse)
		if err != nil {
			return err
		}
	}
	return errorResponse
}
