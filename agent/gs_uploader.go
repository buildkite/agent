package agent

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/mime"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	storage "google.golang.org/api/storage/v1"
)

type GSUploaderConfig struct {
	// The destination which includes the GS bucket name and the path.
	// gs://my-bucket-name/foo/bar
	Destination string

	// Whether or not HTTP calls shoud be debugged
	DebugHTTP bool
}

type GSUploader struct {
	// The configuration
	conf GSUploaderConfig

	// The logger instance to use
	logger *logger.Logger

	// The GS service
	service *storage.Service
}

func NewGSUploader(l *logger.Logger, conf GSUploaderConfig) (*GSUploader, error) {
	client, err := newGoogleClient(storage.DevstorageFullControlScope)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Error creating Google Cloud Storage client: %v", err))
	}
	service, err := storage.New(client)
	if err != nil {
		return nil, err
	}
	return &GSUploader{
		conf: conf,
		logger: l,
		service: service,
	}, nil
}

func newGoogleClient(scope string) (*http.Client, error) {
	if os.Getenv("BUILDKITE_GS_APPLICATION_CREDENTIALS") != "" {
		data, err := ioutil.ReadFile(os.Getenv("BUILDKITE_GS_APPLICATION_CREDENTIALS"))
		if err != nil {
			return nil, err
		}
		conf, err := google.JWTConfigFromJSON(data, scope)
		if err != nil {
			return nil, err
		}
		return conf.Client(oauth2.NoContext), nil
	}
	return google.DefaultClient(context.Background(), scope)
}

func (u *GSUploader) URL(artifact *api.Artifact) string {
	host := "storage.googleapis.com"
	if os.Getenv("BUILDKITE_GCS_ACCESS_HOST") != "" {
		host = os.Getenv("BUILDKITE_GCS_ACCESS_HOST")
	}

	var artifactURL = &url.URL{
		Scheme: "https",
		Host:   host,
		Path:   u.BucketName() + "/" + u.artifactPath(artifact),
	}
	return artifactURL.String()
}

func (u *GSUploader) Upload(artifact *api.Artifact) error {
	permission := os.Getenv("BUILDKITE_GS_ACL")

	// The dirtiest validation method ever...
	if permission != "" &&
		permission != "authenticatedRead" &&
		permission != "private" &&
		permission != "projectPrivate" &&
		permission != "publicRead" &&
		permission != "publicReadWrite" {
			return fmt.Errorf("Invalid GS ACL `%s`", permission)
	}

	if permission == "" {
		u.logger.Debug("Uploading \"%s\" to bucket \"%s\" with default permission",
			u.artifactPath(artifact), u.BucketName())
	} else {
		u.logger.Debug("Uploading \"%s\" to bucket \"%s\" with permission \"%s\"",
			u.artifactPath(artifact), u.BucketName(), permission)
	}
	object := &storage.Object{
		Name:               u.artifactPath(artifact),
		ContentType:        u.mimeType(artifact),
		ContentDisposition: u.contentDisposition(artifact),
	}
	file, err := os.Open(artifact.AbsolutePath)
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to open file \"%q\" (%v)", artifact.AbsolutePath, err))
	}
	call := u.service.Objects.Insert(u.BucketName(), object)
	if permission != "" {
		call = call.PredefinedAcl(permission)
	}
	if res, err := call.Media(file, googleapi.ContentType("")).Do(); err == nil {
		u.logger.Debug("Created object %v at location %v\n\n", res.Name, res.SelfLink)
	} else {
		return errors.New(fmt.Sprintf("Failed to PUT file \"%s\" (%v)", u.artifactPath(artifact), err))
	}

	return nil
}

func (u *GSUploader) artifactPath(artifact *api.Artifact) string {
	parts := []string{u.BucketPath(), artifact.Path}

	return strings.Join(parts, "/")
}

func (u *GSUploader) BucketPath() string {
	return strings.Join(u.destinationParts()[1:len(u.destinationParts())], "/")
}

func (u *GSUploader) BucketName() string {
	return u.destinationParts()[0]
}

func (u *GSUploader) destinationParts() []string {
	trimmed := strings.TrimPrefix(u.conf.Destination, "gs://")

	return strings.Split(trimmed, "/")
}

func (u *GSUploader) mimeType(a *api.Artifact) string {
	extension := filepath.Ext(a.Path)
	mimeType := mime.TypeByExtension(extension)

	if mimeType != "" {
		return mimeType
	} else {
		return "binary/octet-stream"
	}
}

func (u *GSUploader) contentDisposition(a *api.Artifact) string {
	return fmt.Sprintf("inline; filename=\"%s\"", filepath.Base(a.Path))
}
