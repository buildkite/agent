package buildkite

import (
	"github.com/buildkite/agent/buildkite/logger"
)

type ArtifactBatchCreator struct {
	// The ID of the Job that these artifacts belong to
	JobID string

	// The API used for communication
	API API

	// All the artifacts that need to be created
	Artifacts []*Artifact
}

func (a *ArtifactBatchCreator) Create() error {
	length := len(a.Artifacts)
	chunks := 10

	// Split into the artifacts into chunks so we're not uploading a ton of
	// files at once.
	for i := 0; i < length; i += chunks {
		j := i + chunks
		if length < j {
			j = length
		}

		artifacts := a.Artifacts[i:j]

		logger.Info("Creating %d/%d artifacts", i+chunks, length)

		err := a.API.Post("jobs/"+a.JobID+"/artifacts", &artifacts, artifacts)
		if err != nil {
			return err
		}
	}

	return nil
}
