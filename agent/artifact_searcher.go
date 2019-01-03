package agent

import (
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
)

type ArtifactSearcher struct {
	// The logger instance to use
	Logger *logger.Logger

	// The APIClient that will be used when uploading jobs
	APIClient *api.Client

	// The ID of the Build that these artifacts belong to
	BuildID string
}

func (a *ArtifactSearcher) Search(query string, scope string) ([]*api.Artifact, error) {
	if scope == "" {
		a.Logger.Info("Searching for artifacts: \"%s\"", query)
	} else {
		a.Logger.Info("Searching for artifacts: \"%s\" within step: \"%s\"", query, scope)
	}

	options := &api.ArtifactSearchOptions{Query: query, Scope: scope}
	artifacts, _, err := a.APIClient.Artifacts.Search(a.BuildID, options)

	return artifacts, err
}
