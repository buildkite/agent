package artifact

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/require"
)

type nextIntervalRecorder struct {
	next time.Duration
}

func (n *nextIntervalRecorder) SetNextInterval(d time.Duration) {
	n.next = d
}

type stubArtifactAPIClient struct {
	createFn func(context.Context, string, *api.ArtifactBatch) (*api.ArtifactBatchCreateResponse, *api.Response, error)
	updateFn func(context.Context, string, []api.ArtifactState) (*api.Response, error)
}

func (s *stubArtifactAPIClient) CreateArtifacts(ctx context.Context, jobID string, batch *api.ArtifactBatch) (*api.ArtifactBatchCreateResponse, *api.Response, error) {
	if s.createFn == nil {
		panic("unexpected CreateArtifacts call")
	}
	return s.createFn(ctx, jobID, batch)
}

func (s *stubArtifactAPIClient) SearchArtifacts(context.Context, string, *api.ArtifactSearchOptions) ([]*api.Artifact, *api.Response, error) {
	return nil, nil, nil
}

func (s *stubArtifactAPIClient) UpdateArtifacts(ctx context.Context, jobID string, states []api.ArtifactState) (*api.Response, error) {
	if s.updateFn == nil {
		panic("unexpected UpdateArtifacts call")
	}
	return s.updateFn(ctx, jobID, states)
}

func TestBatchCreatorIncreasesCreateBatchSizeAfter429(t *testing.T) {
	t.Parallel()

	artifacts := make([]*api.Artifact, 12)
	for i := range artifacts {
		artifacts[i] = &api.Artifact{Path: fmt.Sprintf("artifact-%d", i)}
	}

	var call int
	var batchSizes []int
	apiClient := &stubArtifactAPIClient{
		createFn: func(_ context.Context, _ string, batch *api.ArtifactBatch) (*api.ArtifactBatchCreateResponse, *api.Response, error) {
			batchSizes = append(batchSizes, len(batch.Artifacts))

			if call == 0 {
				call++
				return nil, &api.Response{Response: &http.Response{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests"}}, errors.New("rate limited")
			}

			ids := make([]string, len(batch.Artifacts))
			for i := range ids {
				ids[i] = fmt.Sprintf("id-%d-%d", call, i)
			}
			call++

			return &api.ArtifactBatchCreateResponse{
				ArtifactIDs:          ids,
				InstructionsTemplate: &api.ArtifactUploadInstructions{},
			}, &api.Response{Response: &http.Response{StatusCode: http.StatusCreated, Status: "201 Created"}}, nil
		},
	}

	creator := NewArtifactBatchCreator(logger.Discard, apiClient, BatchCreatorConfig{
		JobID:           "job-id",
		Artifacts:       artifacts,
		CreateBatchSize: 5,
	})

	_, err := creator.Create(t.Context())
	require.NoError(t, err)
	require.Equal(t, []int{5, 5, 7}, batchSizes)
}

func TestUpdateStatesRespectsConfiguredBatchMax(t *testing.T) {
	t.Parallel()

	var updateSizes []int
	apiClient := &stubArtifactAPIClient{
		updateFn: func(_ context.Context, _ string, states []api.ArtifactState) (*api.Response, error) {
			updateSizes = append(updateSizes, len(states))
			return &api.Response{Response: &http.Response{StatusCode: http.StatusOK, Status: "200 OK"}}, nil
		},
	}

	u := &Uploader{
		conf: UploaderConfig{
			JobID:              "job-id",
			UpdateBatchSizeMax: 2,
		},
		logger:    logger.Discard,
		apiClient: apiClient,
	}

	worker := &artifactUploadWorker{
		Uploader: u,
		trackers: map[*api.Artifact]*artifactTracker{},
	}
	for i := 0; i < 5; i++ {
		artifact := &api.Artifact{ID: fmt.Sprintf("artifact-%d", i)}
		worker.trackers[artifact] = &artifactTracker{
			ArtifactState: api.ArtifactState{ID: artifact.ID, State: "finished"},
		}
	}

	err := worker.updateStates(t.Context())
	require.NoError(t, err)
	require.Equal(t, []int{2, 2, 1}, updateSizes)

	for _, tracker := range worker.trackers {
		require.Equal(t, "sent", tracker.State)
	}
}

func TestApplyRetryAfterHeaderSetsRetryInterval(t *testing.T) {
	t.Parallel()

	r := &nextIntervalRecorder{}
	resp := &api.Response{Response: &http.Response{Header: http.Header{"Retry-After": []string{"3"}}}}

	updated := applyRetryAfterHeader(resp, r)
	require.True(t, updated)
	require.Equal(t, 3*time.Second, r.next)
}

func TestApplyRetryAfterHeaderIgnoresInvalidValues(t *testing.T) {
	t.Parallel()

	r := &nextIntervalRecorder{}
	resp := &api.Response{Response: &http.Response{Header: http.Header{"Retry-After": []string{"not-a-number"}}}}

	updated := applyRetryAfterHeader(resp, r)
	require.False(t, updated)
	require.Equal(t, time.Duration(0), r.next)
}
