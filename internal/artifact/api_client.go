package artifact

import (
	"context"

	"github.com/buildkite/agent/v3/api"
)

// APIClient describes the Agent REST API methods used by the artifact package.
type APIClient interface {
	CreateArtifacts(context.Context, string, *api.ArtifactBatch) (*api.ArtifactBatchCreateResponse, *api.Response, error)
	SearchArtifacts(context.Context, string, *api.ArtifactSearchOptions) ([]*api.Artifact, *api.Response, error)
	UpdateArtifacts(context.Context, string, []api.ArtifactState) (*api.Response, error)
}
