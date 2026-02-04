package artifact

import (
	"context"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/roko"
)

type BatchCreatorConfig struct {
	// The ID of the Job that these artifacts belong to
	JobID string

	// All the artifacts that need to be created
	Artifacts []*api.Artifact

	// Where the artifacts are being uploaded to on the command line
	UploadDestination string

	// CreateArtifactsTimeout, sets a context.WithTimeout around the CreateArtifacts API.
	// If it's zero, there's no context timeout and the default HTTP timeout will prevail.
	CreateArtifactsTimeout time.Duration

	// Whether to allow multipart uploads to the BK-hosted bucket.
	AllowMultipart bool
}

type BatchCreator struct {
	// The creation config
	conf BatchCreatorConfig

	// The logger instance to use
	logger logger.Logger

	// The APIClient that will be used when uploading jobs
	apiClient APIClient
}

func NewArtifactBatchCreator(l logger.Logger, ac APIClient, c BatchCreatorConfig) *BatchCreator {
	return &BatchCreator{
		logger:    l,
		conf:      c,
		apiClient: ac,
	}
}

func (a *BatchCreator) Create(ctx context.Context) ([]*api.Artifact, error) {
	length := len(a.conf.Artifacts)
	chunks := 30

	// Split into the artifacts into chunks so we're not uploading a ton of
	// files at once.
	for i := 0; i < length; i += chunks {
		j := min(i+chunks, length)

		// The artifacts that will be uploaded in this chunk
		theseArtifacts := a.conf.Artifacts[i:j]

		// An ID is required so Buildkite can ensure this create
		// operation is idompotent (if we try and upload the same ID
		// twice, it'll just return the previous data and skip the
		// upload)
		batch := &api.ArtifactBatch{
			ID:                 api.NewUUID(),
			Artifacts:          theseArtifacts,
			UploadDestination:  a.conf.UploadDestination,
			MultipartSupported: a.conf.AllowMultipart,
		}

		a.logger.Info("Creating (%d-%d)/%d artifacts", i, j, length)

		timeout := a.conf.CreateArtifactsTimeout

		// Retry the batch upload a couple of times
		r := roko.NewRetrier(
			roko.WithMaxAttempts(10),
			roko.WithStrategy(roko.ExponentialSubsecond(500*time.Millisecond)),
		)
		creation, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) (*api.ArtifactBatchCreateResponse, error) {
			ctxTimeout := ctx
			if timeout != 0 {
				var cancel func()
				ctxTimeout, cancel = context.WithTimeout(ctx, a.conf.CreateArtifactsTimeout)
				defer cancel()
			}

			creation, resp, err := a.apiClient.CreateArtifacts(ctxTimeout, a.conf.JobID, batch)
			// the server returns a 403 code if the artifact has exceeded the service quota
			// Break the retry on any 4xx code except for 429 Too Many Requests.
			if resp != nil && (resp.StatusCode != 429 && resp.StatusCode >= 400 && resp.StatusCode <= 499) {
				a.logger.Warn("Artifact creation failed with status code %d, breaking the retry loop", resp.StatusCode)
				r.Break()
			}
			if err != nil {
				a.logger.Warn("%s (%s)", err, r)
			}

			// after four attempts (0, 1, 2, 3)...
			if r.AttemptCount() == 3 {
				// The short timeout has given us fast feedback on the first couple of attempts,
				// but perhaps the server needs more time to complete the request, so fall back to
				// the default HTTP client timeout.
				a.logger.Debug("CreateArtifacts timeout (%s) removed for subsequent attempts", timeout)
				timeout = 0
			}

			return creation, err
		})
		// Did the batch creation eventually fail?
		if err != nil {
			return nil, err
		}

		// Save the id and instructions to each artifact
		for index, id := range creation.ArtifactIDs {
			theseArtifacts[index].ID = id
			theseArtifacts[index].UploadInstructions = creation.InstructionsTemplate
			if specific := creation.PerArtifactInstructions[id]; specific != nil {
				theseArtifacts[index].UploadInstructions = specific
			}
		}
	}

	return a.conf.Artifacts, nil
}
