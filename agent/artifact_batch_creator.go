package agent

import (
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/retry"
)

type ArtifactBatchCreatorConfig struct {
	// The ID of the Job that these artifacts belong to
	JobID string

	// All the artifacts that need to be created
	Artifacts []*api.Artifact

	// Where the artifacts are being uploaded to on the command line
	UploadDestination string
}

type ArtifactBatchCreator struct {
	// The creation config
	conf ArtifactBatchCreatorConfig

	// The logger instance to use
	logger logger.Logger

	// The APIClient that will be used when uploading jobs
	apiClient APIClient
}

func NewArtifactBatchCreator(l logger.Logger, ac APIClient, c ArtifactBatchCreatorConfig) *ArtifactBatchCreator {
	return &ArtifactBatchCreator{
		logger:    l,
		conf:      c,
		apiClient: ac,
	}
}

func (a *ArtifactBatchCreator) Create() ([]*api.Artifact, error) {
	length := len(a.conf.Artifacts)
	chunks := 30

	// Split into the artifacts into chunks so we're not uploading a ton of
	// files at once.
	for i := 0; i < length; i += chunks {
		j := i + chunks
		if length < j {
			j = length
		}

		// The artifacts that will be uploaded in this chunk
		theseArtifacts := a.conf.Artifacts[i:j]

		// An ID is required so Buildkite can ensure this create
		// operation is idompotent (if we try and upload the same ID
		// twice, it'll just return the previous data and skip the
		// upload)
		batch := &api.ArtifactBatch{
			ID:                api.NewUUID(),
			Artifacts:         theseArtifacts,
			UploadDestination: a.conf.UploadDestination,
		}

		a.logger.Info("Creating (%d-%d)/%d artifacts", i, j, length)

		var creation *api.ArtifactBatchCreateResponse
		var resp *api.Response
		var err error

		// Retry the batch upload a couple of times
		err = retry.Do(func(s *retry.Stats) error {
			creation, resp, err = a.apiClient.CreateArtifacts(a.conf.JobID, batch)
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404 || resp.StatusCode == 500) {
				s.Break()
			}
			if err != nil {
				a.logger.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})

		// Did the batch creation eventually fail?
		if err != nil {
			return nil, err
		}

		// Save the id and instructions to each artifact
		index := 0
		for _, id := range creation.ArtifactIDs {
			theseArtifacts[index].ID = id
			theseArtifacts[index].UploadInstructions = creation.UploadInstructions
			index += 1
		}
	}

	return a.conf.Artifacts, nil
}
