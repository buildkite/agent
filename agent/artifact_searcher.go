package agent

import (
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
)

type ArtifactSearcher struct {
	// The logger instance to use
	logger *logger.Logger

	// The APIClient that will be used when uploading jobs
	apiClient *api.Client

	// The ID of the Build that these artifacts belong to
	buildID string
}

func NewArtifactSearcher(l *logger.Logger, ac *api.Client, buildID string) *ArtifactSearcher {
	return &ArtifactSearcher{
		logger:    l,
		apiClient: ac,
		buildID:   buildID,
	}
}

func (a *ArtifactSearcher) Search(query string, scope string) ([]*api.Artifact, error) {
	if scope == "" {
		a.logger.Info("Searching for artifacts: \"%s\"", query)
	} else {
		a.logger.Info("Searching for artifacts: \"%s\" within step: \"%s\"", query, scope)
	}

	artifacts, _, err := a.apiClient.Artifacts.Search(a.buildID, &api.ArtifactSearchOptions{
		Query: query,
		Scope: scope,
	})

	return artifacts, err
}
