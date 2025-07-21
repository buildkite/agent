package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/system"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/version"
	"github.com/buildkite/roko"
	"github.com/denisbrodbeck/machineid"
)

var (
	cacheMachineInfoOnce sync.Once
	hostname             string
	machineID            string
	osVersionDump        string
)

// ErrJobAcquisitionRejected is a sentinel error used when acquisition fails because
// the job is already acquired/started/finished/cancelled.
var ErrJobAcquisitionRejected = errors.New("job acquisition rejected")

// ErrJobLocked is a sentinel error used when acquisition fails because
// the job is locked (waiting for dependencies).
var ErrJobLocked = errors.New("job is locked")

// Client is a driver for APIClient that adds retry loops and some error
// handling logic.
type Client struct {
	// APIClient is the API client that Client drives.
	APIClient APIClient

	// Logger is used for logging throughout the client.
	Logger logger.Logger

	// RetrySleepFunc overrides the sleep function within roko retries.
	// This is primarily useful for unit tests. It's recommended to leave as nil.
	RetrySleepFunc func(time.Duration)
}

// AcquireJob acquires a specific job from Buildkite.
// It doesn't interpret or run the job - the caller is responsible for that.
// It contains a builtin timeout of 330 seconds and makes up to 7 attempts, backing off exponentially.
func (c *Client) AcquireJob(ctx context.Context, jobID string) (*api.Job, error) {
	c.Logger.Info("Attempting to acquire job %s...", jobID)

	// Timeout the context to prevent the exponential backoff from growing too
	// large if the job is in the waiting state.
	//
	// If there were no delays or jitter, the attempts would happen at t = 0, 1, 2, 4, ..., 128s
	// after the initial one. Therefore, there are 7 attempts taking at least 255s. If the jitter
	// always hit the max of 5s, then another 40s is added to that. This is still comfortably within
	// the timeout of 330s, and the bound seems tight enough so that the agent is not wasting time
	// waiting for a retry that will never happen.
	timeoutCtx, cancel := context.WithTimeout(ctx, 330*time.Second)
	defer cancel()

	// Acquire the job using the ID we were provided.
	// We'll retry as best we can on non 5xx errors, as well as 423 Locked and 429 Too Many Requests.
	// For retryable errors, if available, we'll consume the value of the server-defined `Retry-After` response header
	// to determine our next retry interval.
	// 4xx errors that are not 423 or 429 will not be retried.
	r := roko.NewRetrier(
		roko.WithMaxAttempts(7),
		roko.WithStrategy(roko.Exponential(2*time.Second, 0)),
		roko.WithJitterRange(-1*time.Second, 5*time.Second),
		roko.WithSleepFunc(c.RetrySleepFunc),
	)

	return roko.DoFunc(timeoutCtx, r, func(r *roko.Retrier) (*api.Job, error) {
		aj, resp, err := c.APIClient.AcquireJob(
			timeoutCtx, jobID,
			api.Header{Name: "X-Buildkite-Lock-Acquire-Job", Value: "1"},
			api.Header{Name: "X-Buildkite-Backoff-Sequence", Value: fmt.Sprintf("%d", r.AttemptCount())},
		)
		if err != nil {
			if resp == nil {
				c.Logger.Warn("%s (%s)", err, r)
				return nil, err
			}

			switch {
			case resp.StatusCode == http.StatusLocked:
				// If the API returns with a 423, the job is in the waiting state. Let's try again later.
				warning := fmt.Sprintf("The job is waiting for a dependency: (%s)", err)
				handleRetriableJobAcquisitionError(warning, resp, r, c.Logger)
				return nil, fmt.Errorf("%w: %w", ErrJobLocked, err)

			case resp.StatusCode == http.StatusTooManyRequests:
				// We're being rate limited by the backend. Let's try again later.
				warning := fmt.Sprintf("Rate limited by the backend: %s", err)
				handleRetriableJobAcquisitionError(warning, resp, r, c.Logger)
				return nil, err

			case resp.StatusCode >= 500:
				// It's a 5xx. Probably worth retrying
				warning := fmt.Sprintf("Server error: %s", err)
				handleRetriableJobAcquisitionError(warning, resp, r, c.Logger)
				return nil, err

			case resp.StatusCode == http.StatusUnprocessableEntity:
				// If the API returns with a 422, it usually means that the job is in a state where it can't be acquired -
				// e.g. it's already running on another agent, or has been cancelled, or has already run. Don't retry
				c.Logger.Error("Buildkite rejected the call to acquire the job: %s", err)
				r.Break()

				return nil, fmt.Errorf("%w: %w", ErrJobAcquisitionRejected, err)

			case resp.StatusCode >= 400 && resp.StatusCode < 500:
				// It's some other client error - not 429 or 423, which we retry, or 422, which we don't, but gets a special log message
				// Don't retry it, the odds of success are low
				c.Logger.Error("%s", err)
				r.Break()

				return nil, err

			default:
				c.Logger.Warn("%s (%s)", err, r)
				return nil, err
			}
		}

		return aj, nil
	})
}

func handleRetriableJobAcquisitionError(warning string, resp *api.Response, r *roko.Retrier, logger logger.Logger) {
	// log the warning and the retrier state at the end of this function. if we logged the error before the call to
	// `r.SetNextInterval`, the `Retrying in ...` message wouldn't include the server-set Retry-After, if it was set
	defer func(r *roko.Retrier) { logger.Warn("%s (%s)", warning, r) }(r)

	if resp == nil {
		return
	}

	retryAfter := resp.Header.Get("Retry-After")

	// Only customize the retry interval if the Retry-After header is present. Otherwise, keep using the default retrier settings
	if retryAfter == "" {
		return
	}

	duration, errParseDuration := time.ParseDuration(retryAfter + "s")
	if errParseDuration != nil {
		return // use the default retrier settings
	}

	r.SetNextInterval(duration)
}

// Connect connects the agent to the Buildkite Agent API, retrying up to 10 times with 5
// seconds delay if it fails.
func (c *Client) Connect(ctx context.Context) error {
	c.Logger.Info("Connecting to Buildkite...")

	return roko.NewRetrier(
		roko.WithMaxAttempts(10),
		roko.WithStrategy(roko.Constant(5*time.Second)),
		roko.WithSleepFunc(c.RetrySleepFunc),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		_, err := c.APIClient.Connect(ctx)
		if err != nil {
			c.Logger.Warn("%s (%s)", err, r)
		}
		return err
	})
}

// Disconnect notifies the Buildkite API that this agent worker/session is
// permanently disconnecting. Don't spend long retrying, because we want to
// disconnect as fast as possible.
func (c *Client) Disconnect(ctx context.Context) error {
	c.Logger.Info("Disconnecting...")
	err := roko.NewRetrier(
		roko.WithMaxAttempts(4),
		roko.WithStrategy(roko.Constant(1*time.Second)),
		roko.WithSleepFunc(c.RetrySleepFunc),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		if _, err := c.APIClient.Disconnect(ctx); err != nil {
			c.Logger.Warn("%s (%s)", err, r) // e.g. POST https://...: 500 (Attempt 0/4 Retrying in ..)
			return err
		}
		return nil
	})
	if err != nil {
		// none of the retries worked
		c.Logger.Warn(
			"There was an error sending the disconnect API call to Buildkite. "+
				"If this agent still appears online, you may have to manually stop it (%s)",
			err,
		)
		return err
	}
	c.Logger.Info("Disconnected")
	return nil
}

// FinishJob finishes the job in the Buildkite Agent API. If the FinishJob call
// cannot return successfully, this will retry for a long time.
func (c *Client) FinishJob(ctx context.Context, job *api.Job, finishedAt time.Time, exit ProcessExit, failedChunkCount int, ignoreAgentInDispatches *bool) error {
	job.FinishedAt = finishedAt.UTC().Format(time.RFC3339Nano)
	job.ExitStatus = strconv.Itoa(exit.Status)
	job.Signal = exit.Signal
	job.SignalReason = exit.SignalReason
	job.ChunksFailedCount = failedChunkCount

	c.Logger.Debug("[JobRunner] Finishing job with exit_status=%s, signal=%s and signal_reason=%s",
		job.ExitStatus, job.Signal, job.SignalReason)

	ctx, cancel := context.WithTimeout(ctx, 1*time.Hour)
	defer cancel()

	return roko.NewRetrier(
		// retry for ~a day with exponential backoff
		roko.WithStrategy(roko.ExponentialSubsecond(2*time.Second)),
		roko.WithMaxAttempts(12), // 12 attempts will take 26 minutes
		roko.WithJitter(),
		roko.WithSleepFunc(c.RetrySleepFunc),
	).DoWithContext(ctx, func(retrier *roko.Retrier) error {
		response, err := c.APIClient.FinishJob(ctx, job, ignoreAgentInDispatches)
		if err != nil {
			// If the API returns with a 422, that means that we
			// successfully tried to finish the job, but Buildkite
			// rejected the finish for some reason. This can
			// sometimes mean that Buildkite has cancelled the job
			// before we get a chance to send the final API call
			// (maybe this agent took too long to kill the
			// process).
			// The API may also return a 401 when job tokens
			// are enabled.
			// In either case, we don't want to keep trying
			// to finish the job forever so we'll just bail out and
			// go find some more work to do.
			if response != nil && (response.StatusCode == 422 || response.StatusCode == 401) {
				c.Logger.Warn("Buildkite rejected the call to finish the job (%s)", err)
				retrier.Break()
				return err
			}
			c.Logger.Warn("%s (%s)", err, retrier)
		}

		return err
	})
}

// Register takes an APIClient and registers it with the Buildkite API
// and populates the result of the register call. It retries up to 30 times.
// Options from opts are *not* set on AgentRegisterRequest, but some fields are
// overridden with specific values.
func (c *Client) Register(ctx context.Context, req api.AgentRegisterRequest) (*api.AgentRegisterResponse, error) {
	// Set up some slightly expensive system info once
	cacheMachineInfoOnce.Do(func() { cacheRegisterSystemInfo(c.Logger) })

	// Set some static things to set on the register request
	req.Version = version.Version()
	req.Build = version.BuildNumber()
	req.PID = os.Getpid()
	req.Arch = runtime.GOARCH
	req.MachineID = machineID
	req.Hostname = hostname
	req.OS = osVersionDump

	// Try to register, retrying every 10 seconds for a maximum of 30 attempts (5 minutes)
	r := roko.NewRetrier(
		roko.WithMaxAttempts(30),
		roko.WithStrategy(roko.Constant(10*time.Second)),
		roko.WithSleepFunc(c.RetrySleepFunc),
	)

	registered, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) (*api.AgentRegisterResponse, error) {
		registered, resp, err := c.APIClient.Register(ctx, &req)
		if err != nil {
			if resp != nil && resp.StatusCode == 401 {
				c.Logger.Warn("Buildkite rejected the registration (%s)", err)
				r.Break()
			} else {
				c.Logger.Warn("%s (%s)", err, r)
			}
			return registered, err
		}

		return registered, nil
	})
	if err != nil {
		return registered, err
	}

	c.Logger.Info("Successfully registered agent \"%s\" with tags [%s]", registered.Name,
		strings.Join(registered.Tags, ", "))

	c.Logger.Debug("Ping interval: %ds", registered.PingInterval)
	c.Logger.Debug("Job status interval: %ds", registered.JobStatusInterval)
	c.Logger.Debug("Heartbeat interval: %ds", registered.HeartbeatInterval)

	if registered.Endpoint != "" {
		c.Logger.Debug("Endpoint: %s", registered.Endpoint)
	}

	return registered, nil
}

// StartJob starts the job in the Buildkite Agent API. We'll retry on connection-related
// issues, but if a connection succeeds and we get an client error response back from
// Buildkite, we won't bother retrying. For example, a "no such host" will
// retry, but an HTTP response from Buildkite that isn't retryable won't.
func (c *Client) StartJob(ctx context.Context, job *api.Job, startedAt time.Time) error {
	job.StartedAt = startedAt.UTC().Format(time.RFC3339Nano)

	return roko.NewRetrier(
		roko.WithMaxAttempts(7),
		roko.WithStrategy(roko.Exponential(2*time.Second, 0)),
		roko.WithSleepFunc(c.RetrySleepFunc),
	).DoWithContext(ctx, func(rtr *roko.Retrier) error {
		response, err := c.APIClient.StartJob(ctx, job)
		if err != nil {
			if response != nil && api.IsRetryableStatus(response) {
				c.Logger.Warn("%s (%s)", err, rtr)
				return err
			}
			if api.IsRetryableError(err) {
				c.Logger.Warn("%s (%s)", err, rtr)
				return err
			}

			c.Logger.Warn("Buildkite rejected the call to start the job (%s)", err)
			rtr.Break()
		}

		return err
	})
}

// UploadChunk uploads a log chunk. If a valid chunk cannot be
// uploaded, it will retry for a long time.
func (c *Client) UploadChunk(ctx context.Context, jobID string, chunk *api.Chunk) error {
	// We consider logs to be an important thing, and we shouldn't give up
	// on sending the chunk data back to Buildkite. In the event Buildkite
	// is having downtime or there are connection problems, we'll want to
	// hold onto chunks until it's back online to upload them.
	//
	// This code will retry for a long time until we get back a successful
	// response from Buildkite that it's considered the chunk (a 4xx will be
	// returned if the chunk is invalid, and we shouldn't retry on that)
	ctx, cancel := context.WithTimeout(ctx, 1*time.Hour)
	defer cancel()

	return roko.NewRetrier(
		// retry for ~a day with exponential backoff
		roko.WithStrategy(roko.ExponentialSubsecond(2*time.Second)),
		roko.WithMaxAttempts(12), // 12 attempts will take 26 minutes
		roko.WithJitter(),
		roko.WithSleepFunc(c.RetrySleepFunc),
	).DoWithContext(ctx, func(retrier *roko.Retrier) error {
		response, err := c.APIClient.UploadChunk(ctx, jobID, chunk)
		if err != nil {
			if response != nil && (response.StatusCode >= 400 && response.StatusCode <= 499) {
				c.Logger.Warn("Buildkite rejected the chunk upload (%s)", err)
				retrier.Break()
				return err
			}
			c.Logger.Warn("%s (%s)", err, retrier)
		}

		return err
	})
}

func cacheRegisterSystemInfo(l logger.Logger) {
	var err error

	machineID, err = machineid.ProtectedID("buildkite-agent")
	if err != nil {
		l.Warn("Failed to find unique machine-id: %v", err)
	}

	hostname, err = os.Hostname()
	if err != nil {
		l.Warn("Failed to find hostname: %s", err)
	}

	osVersionDump, err = system.VersionDump(l)
	if err != nil {
		l.Warn("Failed to find OS information: %s", err)
	}
}
