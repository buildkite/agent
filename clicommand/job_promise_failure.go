package clicommand

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/internal/socket"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/buildkite/agent/v3/logger"
	"github.com/urfave/cli"
)

const jobPromiseFailureHelpDescription = `Usage:

    buildkite-agent job promise-failure <exit-status> [options...]

Description:

Promise the current job will finish with a failing exit status. This records
a non-zero exit status that the job is expected to finish with, allowing the
build to begin failing before the job actually completes.

The promise is binding: it sets a floor on the job's outcome. If the job is
later reported as a success, the promised exit status is recorded instead, so
the job still fails. Likewise, if a hard failure was promised but the job
reports a soft-failure status, the promised status is kept. Any other
reported failure is recorded as reported.

Repeated calls with the same exit status are idempotent. Declaring a
different exit status once one is already recorded is rejected. The agent
debounces repeated calls locally, so each exit status is only declared to
the Buildkite API once per job, even if you call this on every test failure.

The command exits non-zero if the promise is not accepted (for example, if
the job is no longer running, or a different exit status was already
promised). Append '|| true' if you would prefer to ignore that in a script.

Example:

    $ buildkite-agent job promise-failure 1
    $ buildkite-agent job promise-failure 42 --reason "detected failing tests"
`

type JobPromiseFailureConfig struct {
	GlobalConfig
	APIConfig

	ExitStatus   string   `cli:"arg:0" label:"exit status" validate:"required"`
	Reason       string   `cli:"reason"`
	RedactedVars []string `cli:"redacted-vars" normalize:"list"`
}

var JobPromiseFailureCommand = cli.Command{
	Name:        "promise-failure",
	Usage:       "Promise a job will finish with a failing exit status",
	Description: jobPromiseFailureHelpDescription,
	Hidden:      true, // hidden until the early-failure feature is generally available
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:  "reason",
			Value: "",
			Usage: "An optional human-readable reason for the promised failure",
		},
		RedactedVars,
	}),
	Action: func(c *cli.Context) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[JobPromiseFailureConfig](context.Background(), c)
		defer done()

		exitStatus, err := strconv.Atoi(cfg.ExitStatus)
		if err != nil {
			return fmt.Errorf("exit status must be an integer: %w", err)
		}
		// Only positive exit statuses are meaningful here: 0 is success, and
		// negative values (such as -1) are reserved for internal use.
		if exitStatus <= 0 {
			return fmt.Errorf("exit status must be a positive integer: a promised failure cannot have a zero (successful) or negative exit status")
		}

		// Always target the current job (BUILDKITE_JOB_ID): the API requires a
		// job token and rejects (403) any other job, and a promised failure only
		// makes sense for the job that's running.
		jobID := os.Getenv("BUILDKITE_JOB_ID")
		if jobID == "" {
			return fmt.Errorf("BUILDKITE_JOB_ID is not set: this command must be run from within a job")
		}

		needles, _, err := redact.NeedlesFromEnv(cfg.RedactedVars)
		if err != nil {
			return err
		}
		reason := cfg.Reason
		if redactedValue := redact.String(reason, needles); redactedValue != reason {
			l.Warnf("The promise-failure reason for job %q contained one or more secrets from environment variables that have been redacted. If this is deliberate, pass --redacted-vars='' or a list of patterns that does not match the variable containing the secret", jobID)
			reason = redactedValue
		}

		// Prefer the Job API: it debounces repeated and concurrent calls (this
		// may be called on every test failure) so the failure is declared at most
		// once successfully, blocking for an accurate result. Declare directly
		// only when the Job API can't be used (--no-job-api, or old Windows
		// without Unix sockets) or can't be reached.
		client, err := jobapi.NewDefaultClient(ctx)
		if err != nil {
			l.Debugf("Job API unavailable, declaring promised failure directly: %v", err)
			return declarePromiseFailureDirectly(ctx, l, cfg, jobID, exitStatus, reason)
		}

		outcome, err := client.DeclarePromiseFailure(ctx, exitStatus, reason)
		if err == nil {
			if outcome == jobapi.PromiseFailureDebounced {
				// Log at debug to avoid spamming job logs on repeated calls.
				l.Debugf("Promised exit status %d already declared for job %s (debounced)", exitStatus, jobID)
			} else {
				l.Infof("Declared promised exit status %d for job %s", exitStatus, jobID)
			}
			return nil
		}

		// The Job API returned a definitive HTTP error: the Buildkite API
		// rejected the declaration (409, 422) or was unreachable after retries
		// (502). Surface it rather than declaring again.
		var apiErr socket.APIErr
		if errors.As(err, &apiErr) {
			return promiseFailureError(apiErr.StatusCode, err)
		}

		// We couldn't reach the Job API (or its response was lost). Declare
		// directly so the promise still lands; the endpoint is idempotent for the
		// same exit status, so a duplicate is safe.
		l.Warnf("Couldn't reach the Job API to declare the promised failure; declaring it directly: %v", err)
		return declarePromiseFailureDirectly(ctx, l, cfg, jobID, exitStatus, reason)
	},
}

// promiseFailureError wraps err with a human-readable message for the Buildkite
// API status code. The command exits non-zero so the failure is visible to
// scripts, which can append '|| true' to ignore it.
func promiseFailureError(status int, err error) error {
	switch status {
	case http.StatusConflict:
		return fmt.Errorf("a different promised exit status has already been declared for this job: %w", err)

	case http.StatusUnprocessableEntity:
		return fmt.Errorf("the job is no longer running and cannot accept a promised failure: %w", err)
	}

	return fmt.Errorf("failed to declare promised job failure: %w", err)
}

// declarePromiseFailureDirectly declares a promised failure straight to the
// Buildkite API, without debouncing via the Job API. It's used as a fallback
// when the Job API can't be used or reached.
func declarePromiseFailureDirectly(ctx context.Context, l logger.Logger, cfg JobPromiseFailureConfig, jobID string, exitStatus int, reason string) error {
	client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

	req := &api.JobPromiseFailureRequest{
		ExitStatus: exitStatus,
		Reason:     reason,
	}

	status, err := client.PromiseFailureWithRetry(ctx, jobID, req, l.Warnf)
	if err != nil {
		return promiseFailureError(status, err)
	}

	l.Infof("Declared promised exit status %d for job %s", exitStatus, jobID)
	return nil
}
