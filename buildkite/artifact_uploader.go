package buildkite

import (
	"github.com/buildkite/agent/buildkite/logger"
)

type ArtifactUploader struct {
	// The ID of the Job
	JobID string

	// The path of the uploads
	Paths string

	// Where we'll be uploading artifacts
	Destination string

	// The API used for communication
	API API
}

func (a *ArtifactUploader) Upload() error {
	// Set the agent options
	var agent Agent

	// Client specific options
	agent.Client.AuthorizationToken = a.API.Token
	agent.Client.URL = a.API.Endpoint

	// Create artifact structs for all the files we need to upload
	artifacts, err := CollectArtifacts(a.Paths)
	if err != nil {
		return err
	}

	if len(artifacts) == 0 {
		logger.Info("No files matched paths: %s", a.Paths)
	} else {
		logger.Info("Found %d files that match \"%s\"", len(artifacts), a.Paths)

		err := UploadArtifacts(agent.Client, a.JobID, artifacts, a.Destination)
		if err != nil {
			return err
		}
	}

	return nil
}
