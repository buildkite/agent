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

Declare a promised (early) failure for the current job. This records a
non-zero exit status that the job is expected to finish with, allowing the
build to begin failing before the job actually completes. The job keeps
running and finishes normally; only the build-failing cascade starts early.

Repeated calls with the same exit status are idempotent. Declaring a
different exit status once one is already recorded is rejected.

This command requires the early-failure feature to be enabled for your
organization. If it is not, the command logs a warning and exits
successfully, so it can safely be added to pipelines ahead of rollout.

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
	Usage:       "Declare a promised (early) failure for a job",
	Description: jobPromiseFailureHelpDescription,
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
		if exitStatus == 0 {
			return fmt.Errorf("exit status must be non-zero: a promised failure cannot have a successful (0) exit status")
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
			// These statuses are expected during a gradual rollout and aren't
			// worth failing the build over, so warn and exit successfully.
			switch {
			case api.IsErrHavingStatus(err, http.StatusNotFound):
				// The early-failure feature is not enabled for this organization.
				l.Warnf("Promised job failure is not enabled for this organization; skipping declaration")
				return nil

			case api.IsErrHavingStatus(err, http.StatusConflict):
				// A different promised exit status was already declared; the
				// first declaration already took effect.
				l.Warnf("A different promised exit status has already been declared for this job; ignoring")
				return nil

			case api.IsErrHavingStatus(err, http.StatusUnprocessableEntity):
				// Most likely the job is no longer running (cancelled or timed
				// out) by the time this declaration arrived: a benign race.
				l.Warnf("Could not declare promised failure (the job may no longer be running); ignoring")
				return nil
			}

			return fmt.Errorf("failed to declare promised job failure: %w", err)
		}

		l.Infof("Declared promised exit status %d for job %s", exitStatus, cfg.Job)
		return nil
	},
}
