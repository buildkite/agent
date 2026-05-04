package artifact

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/roko"
)

type Searcher struct {
	// The logger instance to use
	logger *slog.Logger

	// The APIClient that will be used when uploading jobs
	apiClient APIClient

	// The ID of the Build that these artifacts belong to
	buildID string
}

func NewSearcher(l *slog.Logger, ac APIClient, buildID string) *Searcher {
	return &Searcher{
		logger:    l,
		apiClient: ac,
		buildID:   buildID,
	}
}

func (a *Searcher) Search(ctx context.Context, query, scope string, includeRetriedJobs, includeDuplicates bool) ([]*api.Artifact, error) {
	if scope == "" {
		a.logger.InfoContext(ctx, fmt.Sprintf("Searching for artifacts: \"%s\"", query))
	} else {
		a.logger.InfoContext(ctx, fmt.Sprintf("Searching for artifacts: \"%s\" within step: \"%s\"", query, scope))
	}

	// Retry on transport errors, a failed search will return 0 artifacts
	r := roko.NewRetrier(
		roko.WithMaxAttempts(10),
		roko.WithStrategy(roko.Constant(5*time.Second)),
	)
	return roko.DoFunc(ctx, r, func(*roko.Retrier) ([]*api.Artifact, error) {
		artifacts, _, err := a.apiClient.SearchArtifacts(ctx, a.buildID, &api.ArtifactSearchOptions{
			Query:              query,
			Scope:              scope,
			State:              "finished",
			IncludeRetriedJobs: includeRetriedJobs,
			IncludeDuplicates:  includeDuplicates,
		})
		return artifacts, err
	})
}
