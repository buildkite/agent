package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/roko"
)

const (
	defaultAttempts                 = 60
	defaultSleepDuration            = 5 * time.Second
	defaultSleepAfterUploadDuration = time.Second
)

var locationRegex = regexp.MustCompile(`jobs/(?P<jobID>[^/]+)/pipelines/(?P<uploadUUID>[^/]+)`)

// PipelineUploader contains the data needed to upload a pipeline to Buildkite
type PipelineUploader struct {
	Client         *api.Client
	Change         *api.PipelineChange
	JobID          string
	RetrySleepFunc func(time.Duration)
}

// Upload will first attempt to perform an async pipeline upload and, depending on the API's
// response, it will poll for the upload's status.
//
// There are 3 "routes" that are relevant
// 1. Async Route:  /jobs/:job_uuid/pipelines?async=true
// 2. Sync Route: /jobs/:job_uuid/pipelines
// 3. Status Route: /jobs/:job_uuid/pipelines/:upload_uuid
//
// In this method, the agent will first upload the pipeline to the Async Route.
// Then, depending on the response it will behave differetly
//
// 1. The Async Route responds 202: poll the Status Route until the upload has beed "applied"
// 2. The Async Route responds with other 2xx: exit, the upload succeeded synchronously (possibly after retry)
// 3. The Async Route responds with other xxx: retry uploading the pipeline to the Async Route
//
// Note that the Sync Route is not used by this version of the agent at all. Typically, the Aysnc
// Route will return 202 whether or not the pipeline upload has been processed.
//
// However, the API has the option to treat the Async Route as if it were the Sync Route by
// returning a 2xx that's not a 202. This will tigger option 2. While the API currently does not do
// this, we want to maintain the flexbitity to do so in the future. If that is implemented, the
// Status Route will not be polled, and either the Async Route will be retried until a (non 202) 2xx
// is returned from the API, or the method will exit early with no error. This reiterates option 2.
//
// If, during a retry loop in option 3, the API returns a 2xx that is a 202, then we assume the API
// changed to supporting Async Uploads between retries and option 1 will be taken.
func (u *PipelineUploader) Upload(ctx context.Context, l logger.Logger) error {
	result, err := u.pipelineUploadAsyncWithRetry(ctx, l)
	if err != nil {
		return fmt.Errorf("failed to upload and accept pipeline: %w", err)
	}

	// If the route does not support async uploads, and it did not error, then the pipeline
	// upload completed successfully, either synchronously in 1 attempt or after re-uploading it
	// in a retry loop.
	if !result.apiIsAsync {
		return nil
	}

	time.Sleep(result.sleepDuration)

	jobIDFromResponse, uuidFromResponse, err := extractJobIdUUID(result.pipelineStatusURL.String())
	if err != nil {
		return fmt.Errorf("failed to parse location to check status of pipeline: %w", err)
	}

	if jobIDFromResponse != u.JobID {
		return fmt.Errorf(
			"jobID from API: %q does not match request: %s",
			jobIDFromResponse,
			u.JobID,
		)
	}

	if uuidFromResponse != u.Change.UUID {
		return fmt.Errorf(
			"pipeline upload UUID from API: %q does not match request: %s",
			uuidFromResponse,
			u.Change.UUID,
		)
	}

	if err := u.pollForPiplineUploadStatus(ctx, l); err != nil {
		return fmt.Errorf("failed to upload and process pipeline: %w", err)
	}

	return nil
}

type pipelineUploadAsyncResult struct {
	pipelineStatusURL *url.URL
	// This will be true iff the api responds with 202
	apiIsAsync    bool
	sleepDuration time.Duration
}

func (u *PipelineUploader) pipelineUploadAsyncWithRetry(
	ctx context.Context,
	l logger.Logger,
) (*pipelineUploadAsyncResult, error) {
	// Retry the pipeline upload a few times before giving up

	r := roko.NewRetrier(
		roko.WithMaxAttempts(defaultAttempts),
		roko.WithStrategy(roko.Constant(defaultSleepDuration)),
		roko.WithSleepFunc(u.RetrySleepFunc),
	)
	return roko.DoFunc(ctx, r, func(r *roko.Retrier) (*pipelineUploadAsyncResult, error) {
		resp, err := u.Client.UploadPipeline(
			ctx,
			u.JobID,
			u.Change,
			api.Header{
				Name:  "X-Buildkite-Backoff-Sequence",
				Value: strconv.Itoa(r.AttemptCount()),
			},
		)
		if err != nil {
			l.Warn("%s (%s)", err, r)

			if jsonerr := new(json.MarshalerError); errors.As(err, &jsonerr) {
				l.Error("Unrecoverable error, skipping retries")
				r.Break()
				return nil, err
			}

			// 422 responses will always fail no need to retry
			if api.IsErrHavingStatus(err, http.StatusUnprocessableEntity) {
				l.Error("Unrecoverable error, skipping retries")
				r.Break()
				return nil, err
			}

			// 529 or other non 2xx
			return nil, err
		}

		result := new(pipelineUploadAsyncResult)

		// An API that has the async upload feature enabled will return 202 with a Location header.
		// Otherwise, if there was no error, then the upload is done.
		if resp.StatusCode == http.StatusAccepted {
			result.apiIsAsync = true
		} else {
			return result, nil
		}

		// If the API supported async uploads, we need to extract the location to poll for the
		// upload's status, after sleeping for a bit to allow the upload to have processed
		if result.sleepDuration, err = time.ParseDuration(resp.Header.Get("Retry-After") + "s"); err != nil {
			result.sleepDuration = defaultSleepAfterUploadDuration
		}

		if result.pipelineStatusURL, err = resp.Location(); err != nil {
			l.Warn("%s (%s)", err, r)
			return nil, err
		}

		return result, nil
	})
}

func (u *PipelineUploader) pollForPiplineUploadStatus(ctx context.Context, l logger.Logger) error {
	return roko.NewRetrier(
		roko.WithMaxAttempts(defaultAttempts),
		roko.WithStrategy(roko.Constant(defaultSleepDuration)),
		roko.WithSleepFunc(u.RetrySleepFunc),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		uploadStatus, resp, err := u.Client.PipelineUploadStatus(
			ctx,
			u.JobID,
			u.Change.UUID,
			api.Header{
				Name:  "X-Buildkite-Backoff-Sequence",
				Value: strconv.Itoa(r.AttemptCount()),
			},
		)
		if err != nil {
			l.Warn("%s (%s)", err, r)

			// 422 responses will always fail no need to retry
			if api.IsErrHavingStatus(err, http.StatusUnprocessableEntity) {
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
			err := fmt.Errorf("pipeline upload not yet applied: %s", uploadStatus.State)
			l.Info("%s (%s)", err, r)
			return err
		case "rejected", "failed":
			l.Error("Unrecoverable error, skipping retries")
			r.Break()
			return fmt.Errorf("pipeline upload %s: %s", uploadStatus.State, uploadStatus.Message)
		default:
			l.Error("Unrecoverable error, skipping retries")
			r.Break()
			return fmt.Errorf("unexpected pipeline upload state from API: %s", uploadStatus.State)
		}
	})
}

type errLocationParse struct {
	location string
}

func (e *errLocationParse) Error() string {
	return fmt.Sprintf("could not extract job and upload UUIDs from Location %s", e.location)
}

func extractJobIdUUID(location string) (string, string, error) {
	matches := locationRegex.FindStringSubmatch(location)
	jobIDIndex := locationRegex.SubexpIndex("jobID")
	uuidIndex := locationRegex.SubexpIndex("uploadUUID")
	if jobIDIndex < 0 || jobIDIndex >= len(matches) || uuidIndex < 0 || uuidIndex >= len(matches) {
		return "", "", &errLocationParse{location: location}
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
