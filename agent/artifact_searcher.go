package agent

import (
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/retry"
)

type ArtifactSearcher struct {
	// The logger instance to use
	logger logger.Logger

	// The APIClient that will be used when uploading jobs
	apiClient APIClient

	// The ID of the Build that these artifacts belong to
	buildID string
}

func NewArtifactSearcher(l logger.Logger, ac APIClient, buildID string) *ArtifactSearcher {
	return &ArtifactSearcher{
		logger:    l,
		apiClient: ac,
		buildID:   buildID,
	}
}

func (a *ArtifactSearcher) Search(query string, scope string, includeRetriedJobs bool, includeDuplicates bool) ([]*api.Artifact, error) {
	if scope == "" {
		a.logger.Info("Searching for artifacts: \"%s\"", query)
	} else {
		a.logger.Info("Searching for artifacts: \"%s\" within step: \"%s\"", query, scope)
	}

	var artifacts []*api.Artifact

	// Retry on transport errors, a failed search will return 0 artifacts
	err := retry.Do(func(s *retry.Stats) error {
		var searchErr error
		artifacts, _, searchErr = a.apiClient.SearchArtifacts(a.buildID, &api.ArtifactSearchOptions{
			Query:              query,
			Scope:              scope,
			IncludeRetriedJobs: includeRetriedJobs,
			IncludeDuplicates:  includeDuplicates,
		})
		return searchErr
	}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})

	return artifacts, err
}
