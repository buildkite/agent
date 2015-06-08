package agent

import (
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
	"time"
)

type ArtifactBatchCreator struct {
	// The APIClient that will be used when uploading jobs
	APIClient *api.Client

	// The ID of the Job that these artifacts belong to
	JobID string

	// All the artifacts that need to be created
	Artifacts []*api.Artifact
}

func (a *ArtifactBatchCreator) Create() ([]*api.Artifact, error) {
	length := len(a.Artifacts)
	chunks := 10
	uploaded := []*api.Artifact{}

	// Split into the artifacts into chunks so we're not uploading a ton of
	// files at once.
	for i := 0; i < length; i += chunks {
		j := i + chunks
		if length < j {
			j = length
		}

		artifacts := a.Artifacts[i:j]

		logger.Info("Creating (%d-%d)/%d artifacts", i, j, length)

		var u []*api.Artifact
		var err error

		// Retry the batch upload a couple of times
		err = retry.Do(func(s *retry.Stats) error {
			u, _, err = a.APIClient.Artifacts.Create(a.JobID, artifacts)
			if err != nil {
				logger.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 1 * time.Second})
		if err != nil {
			return nil, err
		}

		uploaded = append(uploaded, u...)
	}

	return uploaded, nil
}
