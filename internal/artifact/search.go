package artifact

import (
	"context"
	"time"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/roko"
)

type SearchConfig struct {
	BuildID            string
	Query              string
	Scope              string
	IncludeRetriedJobs bool
	IncludeDuplicates  bool
}

type Search struct {
	// The search config
	config SearchConfig

	// The logger instance to use
	logger logger.Logger

	// The agent.APIClient that will be used when uploading jobs
	apiClient agent.APIClient
}

func NewSearch(l logger.Logger, ac agent.APIClient, config SearchConfig) *Search {
	return &Search{
		logger:    l,
		apiClient: ac,
	}
}

func (a *Search) Do(ctx context.Context) ([]*api.Artifact, error) {
	if a.config.Scope == "" {
		a.logger.Info("Searching for artifacts: \"%s\"", a.config.Query)
	} else {
		a.logger.Info("Searching for artifacts: \"%s\" within step: \"%s\"", a.config.Query, a.config.Scope)
	}

	var artifacts []*api.Artifact

	// Retry on transport errors, a failed search will return 0 artifacts
	err := roko.NewRetrier(
		roko.WithMaxAttempts(10),
		roko.WithStrategy(roko.Constant(5*time.Second)),
	).DoWithContext(ctx, func(*roko.Retrier) error {
		var searchErr error
		artifacts, _, searchErr = a.apiClient.SearchArtifacts(ctx, a.config.BuildID, &api.ArtifactSearchOptions{
			Query:              a.config.Query,
			Scope:              a.config.Scope,
			State:              "finished",
			IncludeRetriedJobs: a.config.IncludeRetriedJobs,
			IncludeDuplicates:  a.config.IncludeDuplicates,
		})
		return searchErr
	})

	return artifacts, err
}
