package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/roko"
)

const (
	defatultAttempts                = 60
	defaultSleepDuration            = 5 * time.Second
	defaultSleepAfterUploadDuration = time.Second
)

var (
	locationRegex = regexp.MustCompile(`jobs/(?P<jobID>[^/]+)/pipelines/(?P<uploadUUID>[^/]+)`)
	ErrRegex      = func(s string, r *regexp.Regexp) error { return fmt.Errorf("regex %s failed to match %s", r, s) }
)

type PipelineUploader struct {
	Client         APIClient
	Change         *api.PipelineChange
	JobID          string
	RetrySleepFunc func(time.Duration)
}

func (u *PipelineUploader) AsyncUploadFlow(ctx context.Context, l logger.Logger) error {
	result, err := u.pipelineUploadAsyncWithRetry(ctx, l)
	if err != nil {
		return fmt.Errorf("Failed to upload and accept pipeline: %w", err)
	}

	if result.revertToSyncUpload {
		return u.pipelineUploadWithRetry(ctx, l)
	}

	time.Sleep(result.sleepDuration)

	jobIDFromResponse, uuidFromResponse, err := extractJobIdUUID(result.pipelineStatusURL.String())
	if err != nil {
		return fmt.Errorf("Failed to parse location to check status of pipeline: %w", err)
	}

	if jobIDFromResponse != u.JobID {
		return fmt.Errorf(
			"JobID from API: %s does not match request: %s",
			jobIDFromResponse,
			u.JobID,
		)
	}

	if uuidFromResponse != u.Change.UUID {
		return fmt.Errorf(
			"Pipeline Upload UUID from API: %s does not match request: %s",
			uuidFromResponse,
			u.Change.UUID,
		)
	}

	if err := u.pollForPiplineUploadStatus(ctx, l); err != nil {
		return fmt.Errorf("Failed to upload and process pipeline: %w", err)
	}

	return nil
}

type pipelineUploadAsyncResult struct {
	pipelineStatusURL  *url.URL
	revertToSyncUpload bool
	sleepDuration      time.Duration
}

// TODO: remove this once we are happy that the AsyncUploadFlow works
func (u *PipelineUploader) pipelineUploadWithRetry(ctx context.Context, l logger.Logger) error {
	// Retry the pipeline upload a few times before giving up
	return roko.NewRetrier(
		roko.WithMaxAttempts(defatultAttempts),
		roko.WithStrategy(roko.Constant(defaultSleepDuration)),
		roko.WithSleepFunc(u.RetrySleepFunc),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		_, err := u.Client.UploadPipeline(
			ctx,
			u.JobID,
			u.Change,
			api.Header{
				Name:  "X-Buildkite-Backoff-Sequence",
				Value: fmt.Sprintf("%d", r.AttemptCount()),
			},
		)
		if err != nil {
			l.Warn("%s (%s)", err, r)

			if jsonerr := new(json.MarshalerError); errors.As(err, &jsonerr) {
				l.Error("Unrecoverable error, skipping retries")
				r.Break()
				return err
			}

			// 422 responses will always fail no need to retry
			if apierr := new(
				api.ErrorResponse,
			); errors.As(err, &apierr) && apierr.Response.StatusCode == http.StatusUnprocessableEntity {
				l.Error("Unrecoverable error, skipping retries")
				r.Break()
				return err
			}

			return err
		}

		return nil
	})
}

func (u *PipelineUploader) pipelineUploadAsyncWithRetry(
	ctx context.Context,
	l logger.Logger,
) (*pipelineUploadAsyncResult, error) {
	result := &pipelineUploadAsyncResult{}
	// Retry the pipeline upload a few times before giving up
	if err := roko.NewRetrier(
		roko.WithMaxAttempts(defatultAttempts),
		roko.WithStrategy(roko.Constant(defaultSleepDuration)),
		roko.WithSleepFunc(u.RetrySleepFunc),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		resp, err := u.Client.UploadPipelineAsync(
			ctx,
			u.JobID,
			u.Change,
			api.Header{
				Name:  "X-Buildkite-Backoff-Sequence",
				Value: fmt.Sprintf("%d", r.AttemptCount()),
			},
		)
		if err != nil {
			l.Warn("%s (%s)", err, r)

			if jsonerr := new(json.MarshalerError); errors.As(err, &jsonerr) {
				l.Error("Unrecoverable error, skipping retries")
				r.Break()
				return err
			}

			// 422 responses will always fail no need to retry
			if apierr := new(
				api.ErrorResponse,
			); errors.As(err, &apierr) && apierr.Response.StatusCode == http.StatusUnprocessableEntity {
				l.Error("Unrecoverable error, skipping retries")
				r.Break()
				return err
			}

			return err
		}

		// An API that has the AsyncUploadFlow enabled will return 202 with a Location header
		// Otherwise, the API is telling us to fall back to the previous pipeline upload flow
		if resp.StatusCode != http.StatusAccepted {
			l.Warn("Falling out of async pipeline upload flow, the pipeline will be re-uploaded on each retry.")
			result.revertToSyncUpload = true
			return nil
		}

		if result.sleepDuration, err = time.ParseDuration(resp.Header.Get("Retry-After") + "s"); err != nil {
			result.sleepDuration = defaultSleepAfterUploadDuration
		}

		if result.pipelineStatusURL, err = resp.Location(); err != nil {
			l.Warn("%s (%s)", err, r)
			return err
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return result, nil
}

func (u *PipelineUploader) pollForPiplineUploadStatus(ctx context.Context, l logger.Logger) error {
	return roko.NewRetrier(
		roko.WithMaxAttempts(defatultAttempts),
		roko.WithStrategy(roko.Constant(defaultSleepDuration)),
		roko.WithSleepFunc(u.RetrySleepFunc),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		uploadStatus, resp, err := u.Client.PipelineUploadStatus(
			ctx,
			u.JobID,
			u.Change.UUID,
			api.Header{
				Name:  "X-Buildkite-Backoff-Sequence",
				Value: fmt.Sprintf("%d", r.AttemptCount()),
			},
		)
		if err != nil {
			l.Warn("%s (%s)", err, r)

			// 422 responses will always fail no need to retry
			if apierr := new(
				api.ErrorResponse,
			); errors.As(err, &apierr) && apierr.Response.StatusCode == http.StatusUnprocessableEntity {
				l.Error("Unrecoverable error, skipping retries")
				r.Break()
				return err
			}

			setNextIntervalFromResponse(r, resp)
			return err
		}

		switch uploadStatus.State {
		case "applied":
			return nil
		case "pending", "processing":
			setNextIntervalFromResponse(r, resp)
			err := fmt.Errorf("Pipeline upload not yet applied: %s", uploadStatus.State)
			l.Warn("%s (%s)", err, r)
			return err
		case "rejected", "failed":
			l.Error("Unrecoverable error, skipping retries")
			r.Break()
			return fmt.Errorf("Pipeline upload %s: %s", uploadStatus.State, uploadStatus.Message)
		default:
			l.Error("Unrecoverable error, skipping retries")
			r.Break()
			return fmt.Errorf("Unexpected pipeline upload state from API: %s", uploadStatus.State)
		}
	})
}

func extractJobIdUUID(location string) (string, string, error) {
	matches := locationRegex.FindStringSubmatch(location)
	jobIDIndex := locationRegex.SubexpIndex("jobID")
	uuidIndex := locationRegex.SubexpIndex("uploadUUID")
	if jobIDIndex < 0 || jobIDIndex >= len(matches) || uuidIndex < 0 || uuidIndex >= len(matches) {
		return "", "", ErrRegex(location, locationRegex)
	}
	return matches[jobIDIndex], matches[uuidIndex], nil
}

// If a "Retry-After" Header is set, sets the next retry interval to that value in seconds,
// otherwise, does nothing to the retrier
func setNextIntervalFromResponse(r *roko.Retrier, resp *api.Response) {
	if r == nil || resp == nil {
		return
	}

	duration, err := time.ParseDuration(resp.Header.Get("Retry-After") + "s")
	if err == nil {
		r.SetNextInterval(duration)
	}
}
