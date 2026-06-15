package clicommand

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/roko"
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
different exit status once one is already recorded is rejected.

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
	Job          string   `cli:"job" validate:"required"`
	RedactedVars []string `cli:"redacted-vars" normalize:"list"`
}

var JobPromiseFailureCommand = cli.Command{
	Name:        "promise-failure",
	Usage:       "Promise a job will finish with a failing exit status",
	Description: jobPromiseFailureHelpDescription,
	Hidden:      true, // hidden until the early-failure feature is generally available
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "The job to declare an early failure for. Defaults to the current job",
			EnvVar: "BUILDKITE_JOB_ID",
		},
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

		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		needles, _, err := redact.NeedlesFromEnv(cfg.RedactedVars)
		if err != nil {
			return err
		}
		if redactedValue := redact.String(cfg.Reason, needles); redactedValue != cfg.Reason {
			l.Warnf("The reason for job %q contained one or more secrets from environment variables that have been redacted. If this is deliberate, pass --redacted-vars='' or a list of patterns that does not match the variable containing the secret", cfg.Job)
			cfg.Reason = redactedValue
		}

		req := &api.JobPromiseFailureRequest{
			ExitStatus: exitStatus,
			Reason:     cfg.Reason,
		}

		err = roko.NewRetrier(
			roko.WithMaxAttempts(10),
			roko.WithStrategy(roko.ExponentialSubsecond(2*time.Second)),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			resp, err := client.PromiseFailure(ctx, cfg.Job, req)
			if api.BreakOnNonRetryable(r, resp, err) {
				return err
			}
			if err != nil {
				l.Warnf("%s (%s)", err, r)
				return err
			}
			return nil
		})
		if err != nil {
			// The promise wasn't accepted. Exit non-zero so the outcome is
			// visible to scripts; callers who consider a given case acceptable
			// can append '|| true'.
			switch {
			case api.IsErrHavingStatus(err, http.StatusNotFound):
				return fmt.Errorf("promised failures are not enabled for this organization: %w", err)

			case api.IsErrHavingStatus(err, http.StatusConflict):
				return fmt.Errorf("a different promised exit status has already been declared for this job: %w", err)

			case api.IsErrHavingStatus(err, http.StatusUnprocessableEntity):
				return fmt.Errorf("the job is no longer running and cannot accept a promised failure: %w", err)
			}

			return fmt.Errorf("failed to declare promised job failure: %w", err)
		}

		l.Infof("Declared promised exit status %d for job %s", exitStatus, cfg.Job)
		return nil
	},
}
