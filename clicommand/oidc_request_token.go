package clicommand

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

type OIDCTokenConfig struct {
	GlobalConfig
	APIConfig

	Audience      string `cli:"audience"`
	Lifetime      int    `cli:"lifetime"`
	Job           string `cli:"job"      validate:"required"`
	SkipRedaction bool   `cli:"skip-redaction"`
	// TODO: enumerate possible values, perhaps by adding a link to the documentation
	Claims         []string `cli:"claim"           normalize:"list"`
	AWSSessionTags []string `cli:"aws-session-tag" normalize:"list"`
}

const (
	backoffSeconds       = 2
	maxAttempts          = 5
	oidcTokenDescription = `Usage:

    buildkite-agent oidc request-token [options...]

Description:

Requests and prints an OIDC token from Buildkite that claims the Job ID
(amongst other things) and the specified audience. If no audience is
specified, the endpoint's default audience will be claimed.

Example:

    $ buildkite-agent oidc request-token --audience sts.amazonaws.com

Requests and prints an OIDC token from Buildkite that claims the Job ID
(amongst other things) and the audience "sts.amazonaws.com".`
)

var OIDCRequestTokenCommand = cli.Command{
	Name:        "request-token",
	Usage:       "Requests and prints an OIDC token from Buildkite with the specified audience,",
	Description: oidcTokenDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:  "audience",
			Usage: "The audience that will consume the OIDC token. The API will choose a default audience if it is omitted.",
		},
		cli.IntFlag{
			Name:  "lifetime",
			Usage: "The time (in seconds) the OIDC token will be valid for before expiry. Must be a non-negative integer. If the flag is omitted or set to 0, the API will choose a default finite lifetime.",
		},
		cli.StringFlag{
			Name:   "job",
			Usage:  "Buildkite Job Id to claim in the OIDC token",
			EnvVar: "BUILDKITE_JOB_ID",
		},

		cli.StringSliceFlag{
			Name:   "claim",
			Value:  &cli.StringSlice{},
			Usage:  "Claims to add to the OIDC token",
			EnvVar: "BUILDKITE_OIDC_TOKEN_CLAIMS",
		},

		cli.StringSliceFlag{
			Name:   "aws-session-tag",
			Value:  &cli.StringSlice{},
			Usage:  "Add claims as AWS Session Tags",
			EnvVar: "BUILDKITE_OIDC_TOKEN_AWS_SESSION_TAGS",
		},

		cli.BoolFlag{
			Name:   "skip-redaction",
			Usage:  "Skip redacting the OIDC token from the logs. Then, the command will print the token to the Job's logs if called directly.",
			EnvVar: "BUILDKITE_AGENT_OIDC_REQUEST_TOKEN_SKIP_TOKEN_REDACTION",
		},
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[OIDCTokenConfig](ctx, c)
		defer done()

		// Note: if --lifetime is omitted, cfg.Lifetime = 0
		if cfg.Lifetime < 0 {
			return fmt.Errorf("lifetime %d must be a non-negative integer.", cfg.Lifetime)
		}

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		// Request the token
		r := roko.NewRetrier(
			roko.WithMaxAttempts(maxAttempts),
			roko.WithStrategy(roko.Exponential(backoffSeconds*time.Second, 0)),
		)
		token, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) (*api.OIDCToken, error) {
			req := &api.OIDCTokenRequest{
				Job:            cfg.Job,
				Audience:       cfg.Audience,
				Lifetime:       cfg.Lifetime,
				Claims:         cfg.Claims,
				AWSSessionTags: cfg.AWSSessionTags,
			}

			token, resp, err := client.OIDCToken(ctx, req)
			if resp != nil {
				switch resp.StatusCode {
				// Don't bother retrying if the response was one of these statuses
				case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity:
					r.Break()
					return nil, err
				}
			}

			if err != nil {
				l.Warn("%s (%s)", err, r)
			}
			return token, err
		})
		if err != nil {
			if len(cfg.Audience) > 0 {
				l.Error("Could not obtain OIDC token for audience %s", cfg.Audience)
			} else {
				l.Error("Could not obtain OIDC token for default audience")
			}
			return err
		}

		if !cfg.SkipRedaction {
			jobClient, err := jobapi.NewDefaultClient(ctx)
			if err != nil {
				return fmt.Errorf("failed to create Job API client: %w", err)
			}

			if err := AddToRedactor(ctx, l, jobClient, token.Token); err != nil {
				if cfg.Debug {
					return err
				}
				return errOIDCRedact
			}
		}

		_, _ = fmt.Fprintln(c.App.Writer, token.Token)
		return nil
	},
}
