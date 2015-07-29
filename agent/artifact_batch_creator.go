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

	// Where the artifacts are being uploaded to on the command line
	UploadDestination string
}

func (a *ArtifactBatchCreator) Create() ([]*api.Artifact, error) {
	length := len(a.Artifacts)
	chunks := 30

	// Split into the artifacts into chunks so we're not uploading a ton of
	// files at once.
	for i := 0; i < length; i += chunks {
		j := i + chunks
		if length < j {
			j = length
		}

		// The artifacts that will be uploaded in this chunk
		theseArtiacts := a.Artifacts[i:j]

		// An ID is required so Buildkite can ensure this create
		// operation is idompotent (if we try and upload the same ID
		// twice, it'll just return the previous data and skip the
		// upload)
		batch := &api.ArtifactBatch{api.NewUUID(), theseArtiacts, a.UploadDestination}

		logger.Info("Creating (%d-%d)/%d artifacts", i, j, length)

		var creation *api.ArtifactBatchCreateResponse
		var resp *api.Response
		var err error

		// Retry the batch upload a couple of times
		err = retry.Do(func(s *retry.Stats) error {
			creation, resp, err = a.APIClient.Artifacts.Create(a.JobID, batch)
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404 || resp.StatusCode == 500) {
				s.Break()
			}
			if err != nil {
				logger.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 1 * time.Second})

		// Did the batch creation eventually fail?
		if err != nil {
			return nil, err
		}

		// Save the id and instructions to each artifact
		index := 0
		for _, id := range creation.ArtifactIDs {
			theseArtiacts[index].ID = id
			theseArtiacts[index].UploadInstructions = creation.UploadInstructions
			index += 1
		}
	}

	return a.Artifacts, nil
}
