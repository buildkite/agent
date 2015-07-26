package agent

import (
	"time"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
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

		// A UUID is required so Buildkite can ensure this create
		// operation is idompotent (if we try and upload the same UUID
		// twice, it'll just return the previous data and skip the
		// upload)
		batch := &api.ArtifactBatch{api.NewUUID(), a.Artifacts[i:j]}

		logger.Info("Creating (%d-%d)/%d artifacts", i, j, length)

		var b *api.ArtifactBatch
		var err error
		var resp *api.Response

		// Retry the batch upload a couple of times
		err = retry.Do(func(s *retry.Stats) error {
			b, resp, err = a.APIClient.Artifacts.Create(a.JobID, batch)
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404) {
				s.Break()
			}
			if err != nil {
				logger.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 1 * time.Second})
		if err != nil {
			return nil, err
		}

		uploaded = append(uploaded, b.Artifacts...)
	}

	return uploaded, nil
}
