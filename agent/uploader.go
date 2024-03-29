package agent

import (
	"context"

	"github.com/buildkite/agent/v3/api"
)

type Uploader interface {
	// The Artifact.URL property is populated with what ever is returned
	// from this method prior to uploading.
	URL(*api.Artifact) string

	// The actual uploading of the file
	Upload(context.Context, *api.Artifact) error
}
