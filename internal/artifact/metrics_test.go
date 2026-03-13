package artifact

import (
	"context"
	"errors"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type artifactMetricsTestAPIClient struct{}

func (artifactMetricsTestAPIClient) CreateArtifacts(context.Context, string, *api.ArtifactBatch) (*api.ArtifactBatchCreateResponse, *api.Response, error) {
	return nil, nil, nil
}

func (artifactMetricsTestAPIClient) SearchArtifacts(context.Context, string, *api.ArtifactSearchOptions) ([]*api.Artifact, *api.Response, error) {
	return nil, nil, nil
}

func (artifactMetricsTestAPIClient) UpdateArtifacts(context.Context, string, []api.ArtifactState) (*api.Response, error) {
	return nil, nil
}

type artifactMetricsTestWorkUnit struct {
	artifact *api.Artifact
}

func (w artifactMetricsTestWorkUnit) Artifact() *api.Artifact { return w.artifact }
func (artifactMetricsTestWorkUnit) Description() string       { return "test work unit" }
func (artifactMetricsTestWorkUnit) DoWork(context.Context) (*api.ArtifactPartETag, error) {
	return nil, nil
}

func TestArtifactMetricsIncrementOnSuccessfulUpload(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	beforeUploaded := testutil.ToFloat64(artifactsUploaded)
	beforeBytes := testutil.ToFloat64(artifactBytesUploaded)
	beforeFailed := testutil.ToFloat64(artifactUploadFailures)

	artifact := &api.Artifact{ID: "a1", FileSize: 1234}
	worker := &artifactUploadWorker{
		Uploader: &Uploader{
			logger:    logger.Discard,
			apiClient: artifactMetricsTestAPIClient{},
			conf:      UploaderConfig{JobID: "job-1"},
		},
		trackers: map[*api.Artifact]*artifactTracker{
			artifact: {
				pendingWork: 1,
				ArtifactState: api.ArtifactState{
					ID: artifact.ID,
				},
			},
		},
	}

	resultsCh := make(chan workUnitResult, 1)
	errCh := make(chan error, 1)
	go worker.stateUpdater(ctx, resultsCh, errCh)

	resultsCh <- workUnitResult{workUnit: artifactMetricsTestWorkUnit{artifact: artifact}}
	close(resultsCh)

	if err := <-errCh; err != nil {
		t.Fatalf("stateUpdater() error = %v", err)
	}

	if got := testutil.ToFloat64(artifactsUploaded) - beforeUploaded; got != 1 {
		t.Fatalf("artifactsUploaded delta = %v, want 1", got)
	}
	if got := testutil.ToFloat64(artifactBytesUploaded) - beforeBytes; got != 1234 {
		t.Fatalf("artifactBytesUploaded delta = %v, want 1234", got)
	}
	if got := testutil.ToFloat64(artifactUploadFailures) - beforeFailed; got != 0 {
		t.Fatalf("artifactUploadFailures delta = %v, want 0", got)
	}
}

func TestArtifactMetricsIncrementOnFailedUpload(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	beforeUploaded := testutil.ToFloat64(artifactsUploaded)
	beforeBytes := testutil.ToFloat64(artifactBytesUploaded)
	beforeFailed := testutil.ToFloat64(artifactUploadFailures)

	artifact := &api.Artifact{ID: "a2", FileSize: 4321}
	worker := &artifactUploadWorker{
		Uploader: &Uploader{
			logger:    logger.Discard,
			apiClient: artifactMetricsTestAPIClient{},
			conf:      UploaderConfig{JobID: "job-2"},
		},
		trackers: map[*api.Artifact]*artifactTracker{
			artifact: {
				pendingWork: 2,
				ArtifactState: api.ArtifactState{
					ID: artifact.ID,
				},
			},
		},
	}

	resultsCh := make(chan workUnitResult, 2)
	errCh := make(chan error, 1)
	go worker.stateUpdater(ctx, resultsCh, errCh)

	u := artifactMetricsTestWorkUnit{artifact: artifact}
	resultsCh <- workUnitResult{workUnit: u, err: errors.New("first failure")}
	resultsCh <- workUnitResult{workUnit: u, err: errors.New("second failure")}
	close(resultsCh)

	if err := <-errCh; err == nil {
		t.Fatal("stateUpdater() error = nil, want error")
	}

	if got := testutil.ToFloat64(artifactUploadFailures) - beforeFailed; got != 1 {
		t.Fatalf("artifactUploadFailures delta = %v, want 1", got)
	}
	if got := testutil.ToFloat64(artifactsUploaded) - beforeUploaded; got != 0 {
		t.Fatalf("artifactsUploaded delta = %v, want 0", got)
	}
	if got := testutil.ToFloat64(artifactBytesUploaded) - beforeBytes; got != 0 {
		t.Fatalf("artifactBytesUploaded delta = %v, want 0", got)
	}
}
