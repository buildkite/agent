package artifact

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	storage "google.golang.org/api/storage/v1"
)

type GSUploaderConfig struct {
	// The destination which includes the GS bucket name and the path.
	// gs://my-bucket-name/foo/bar
	Destination string
}

type GSUploader struct {
	// The gs bucket path set from the destination
	BucketPath string

	// The gs bucket name set from the destination
	BucketName string

	// The configuration
	conf GSUploaderConfig

	// The logger instance to use
	logger logger.Logger

	// The GS service
	service *storage.Service
}

func NewGSUploader(ctx context.Context, l logger.Logger, c GSUploaderConfig) (*GSUploader, error) {
	client, err := newGoogleClient(ctx, storage.DevstorageFullControlScope)
	if err != nil {
		return nil, fmt.Errorf("creating Google Cloud Storage client: %w", err)
	}
	service, err := storage.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}
	bucketName, bucketPath := ParseGSDestination(c.Destination)
	return &GSUploader{
		BucketPath: bucketPath,
		BucketName: bucketName,
		conf:       c,
		logger:     l,
		service:    service,
	}, nil
}

func ParseGSDestination(destination string) (name, path string) {
	parts := strings.Split(strings.TrimPrefix(string(destination), "gs://"), "/")
	path = strings.Join(parts[1:], "/")
	name = parts[0]
	return name, path
}

func clientFromJSON(ctx context.Context, data []byte, scope string) (*http.Client, error) {
	conf, err := google.JWTConfigFromJSON(data, scope)
	if err != nil {
		return nil, err
	}
	return conf.Client(ctx), nil
}

func newGoogleClient(ctx context.Context, scope string) (*http.Client, error) {
	if os.Getenv("BUILDKITE_GS_APPLICATION_CREDENTIALS_JSON") != "" {
		data := []byte(os.Getenv("BUILDKITE_GS_APPLICATION_CREDENTIALS_JSON"))
		return clientFromJSON(ctx, data, scope)
	}
	if os.Getenv("BUILDKITE_GS_APPLICATION_CREDENTIALS") != "" {
		data, err := os.ReadFile(os.Getenv("BUILDKITE_GS_APPLICATION_CREDENTIALS"))
		if err != nil {
			return nil, err
		}
		return clientFromJSON(ctx, data, scope)
	}
	return google.DefaultClient(ctx, scope)
}

func (u *GSUploader) URL(artifact *api.Artifact) string {
	// Set host and pathPrefix to default values
	host := "storage.googleapis.com"
	pathPrefix := u.BucketName

	// Override default host if required.
	if envHost, ok := os.LookupEnv("BUILDKITE_GCS_ACCESS_HOST"); ok {
		host = envHost
	}

	// If this is set, we trust the user to supply a valid path prefix, instead of using the default GCS path
	if prefix, ok := os.LookupEnv("BUILDKITE_GCS_PATH_PREFIX"); ok {
		pathPrefix = prefix
	}

	// Build the path from the prefix and the artifactPath
	// Also ensure that we always have exactly one / between prefix and artifactPath
	path := path.Join(pathPrefix, u.artifactPath(artifact))

	artifactURL := &url.URL{
		Scheme: "https",
		Host:   host,
		Path:   path,
	}
	return artifactURL.String()
}

func (u *GSUploader) CreateWork(artifact *api.Artifact) ([]workUnit, error) {
	return []workUnit{&gsUploaderWork{
		GSUploader: u,
		artifact:   artifact,
	}}, nil
}

type gsUploaderWork struct {
	*GSUploader
	artifact *api.Artifact
}

func (u *gsUploaderWork) Artifact() *api.Artifact { return u.artifact }

func (u *gsUploaderWork) Description() string {
	return singleUnitDescription(u.artifact)
}

func (u *gsUploaderWork) DoWork(_ context.Context) (*api.ArtifactPartETag, error) {
	permission := os.Getenv("BUILDKITE_GS_ACL")

	// The dirtiest validation method ever...
	if permission != "" &&
		permission != "authenticatedRead" &&
		permission != "private" &&
		permission != "projectPrivate" &&
		permission != "publicRead" &&
		permission != "publicReadWrite" {
		return nil, fmt.Errorf("invalid GS ACL `%s`", permission)
	}

	if permission == "" {
		u.logger.Debug("Uploading \"%s\" to bucket \"%s\" with default permission",
			u.artifactPath(u.artifact), u.BucketName)
	} else {
		u.logger.Debug("Uploading \"%s\" to bucket \"%s\" with permission \"%s\"",
			u.artifactPath(u.artifact), u.BucketName, permission)
	}
	object := &storage.Object{
		Name:               u.artifactPath(u.artifact),
		ContentType:        u.artifact.ContentType,
		ContentDisposition: u.contentDisposition(u.artifact),
	}
	file, err := os.Open(u.artifact.AbsolutePath)
	if err != nil {
		return nil, fmt.Errorf("opening file %q: %w", u.artifact.AbsolutePath, err)
	}
	call := u.service.Objects.Insert(u.BucketName, object)
	if permission != "" {
		call = call.PredefinedAcl(permission)
	}
	if res, err := call.Media(file, googleapi.ContentType("")).Do(); err == nil {
		u.logger.Debug("Created object %v at location %v\n\n", res.Name, res.SelfLink)
	} else {
		return nil, fmt.Errorf("failed to PUT file %q: %w", u.artifactPath(u.artifact), err)
	}

	return nil, nil
}

func (u *GSUploader) artifactPath(artifact *api.Artifact) string {
	parts := []string{u.BucketPath, artifact.Path}

	return strings.Join(parts, "/")
}

func (u *GSUploader) contentDisposition(a *api.Artifact) string {
	return fmt.Sprintf("inline; filename=\"%s\"", filepath.Base(a.Path))
}
