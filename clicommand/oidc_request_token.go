package clicommand

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/jobapi"
	"github.com/buildkite/roko"
	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otel"
)

type OIDCTokenConfig struct {
	GlobalConfig
	APIConfig

	Audience      string `cli:"audience"`
	Lifetime      int    `cli:"lifetime"`
	Job           string `cli:"job"      validate:"required"`
	SkipRedaction bool   `cli:"skip-redaction"`
	Format        string `cli:"format"`
	// TODO: enumerate possible values, perhaps by adding a link to the documentation
	Claims         []string `cli:"claim"           normalize:"list"`
	AWSSessionTags []string `cli:"aws-session-tag" normalize:"list"`
	SubjectClaim   string   `cli:"subject-claim"`
}

// OIDCTokenRefusedExitStatus is the exit status when the Buildkite API
// positively refused to issue an OIDC token: a 400, 401, 403, 404, 410, or
// 422 response — for example an audience disallowed by organization policy.
// Retrying such a request will not succeed.
//
// All other failures — exhausted retries on 5xx, any other 4xx (408, 421,
// 425, 429, nonstandard proxy statuses), network timeouts, and other
// transport errors — exit with the generic status 1 and may succeed if
// retried later. Ambiguity deliberately biases toward the transient exit
// status: a spurious 1 costs callers a pointless retry, whereas a spurious 77
// makes them abandon a credential they could have obtained.
//
// This exit status is a contract consumed by other tools (at least
// github.com/buildkite/test-engine-client, which uses it to decide whether an
// OTLP relay credential failure is terminal). Do not change or reuse it.
const OIDCTokenRefusedExitStatus = 77

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
(amongst other things) and the audience "sts.amazonaws.com".

Exit statuses:

- 0: the token was obtained and printed.
- 77: the Buildkite API refused to issue the token (a 400, 401, 403, 404,
  410, or 422 response, such as a disallowed audience). Retrying will not
  succeed.
- 1: any other failure, including transient API or network errors (timeouts,
  5xx, other 4xx). Retrying may succeed.`
)

var OIDCRequestTokenCommand = &cli.Command{
	Name:        "request-token",
	Usage:       "Requests and prints an OIDC token from Buildkite with the specified audience,",
	Description: oidcTokenDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		&cli.StringFlag{
			Name:  "audience",
			Usage: "The audience that will consume the OIDC token. The API will choose a default audience if it is omitted.",
		},
		&cli.IntFlag{
			Name:  "lifetime",
			Usage: "The time (in seconds) the OIDC token will be valid for before expiry. Must be a non-negative integer. If the flag is omitted or set to 0, the API will choose a default finite lifetime.",
		},
		&cli.StringFlag{
			Name:    "job",
			Usage:   "Buildkite Job Id to claim in the OIDC token",
			Sources: cli.EnvVars("BUILDKITE_JOB_ID"),
		},

		&cli.StringFlag{
			Name:    "subject-claim",
			Usage:   "An immutable claim to use as the token's subject (e.g. pipeline_id, cluster_id). If omitted, the default compound subject is used.",
			Sources: cli.EnvVars("BUILDKITE_OIDC_TOKEN_SUBJECT_CLAIM"),
		},

		&cli.StringSliceFlag{
			Name:    "claim",
			Value:   nil,
			Usage:   "Claims to add to the OIDC token",
			Sources: cli.EnvVars("BUILDKITE_OIDC_TOKEN_CLAIMS"),
		},

		&cli.StringSliceFlag{
			Name:    "aws-session-tag",
			Value:   nil,
			Usage:   "Add claims as AWS Session Tags",
			Sources: cli.EnvVars("BUILDKITE_OIDC_TOKEN_AWS_SESSION_TAGS"),
		},

		&cli.BoolFlag{
			Name:    "skip-redaction",
			Usage:   "Skip redacting the OIDC token from the logs. Then, the command will print the token to the Job's logs if called directly (default: false)",
			Sources: cli.EnvVars("BUILDKITE_AGENT_OIDC_REQUEST_TOKEN_SKIP_TOKEN_REDACTION"),
		},
		&cli.StringFlag{
			Name:  "format",
			Value: "jwt",
			Usage: "The format to output the token in. Supported values are 'jwt' (the default) and 'gcp'. When 'gcp' is specified, the token will be output in a JSON structure compatible with GCP's workload identity federation.",
		},
	}),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[OIDCTokenConfig](ctx, c)
		defer done()
		ctx, span := otel.Tracer("buildkite-agent").Start(ctx, "oidc-request-token")
		defer span.End()

		// Note: if --lifetime is omitted, cfg.Lifetime = 0
		if cfg.Lifetime < 0 {
			return fmt.Errorf("lifetime %d must be a non-negative integer", cfg.Lifetime)
		}

		if cfg.Format != "jwt" && cfg.Format != "gcp" {
			return fmt.Errorf("format %q is not valid. Supported values are 'jwt' and 'gcp'", cfg.Format)
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
				SubjectClaim:   cfg.SubjectClaim,
			}

			token, resp, err := client.OIDCToken(ctx, req)
			if api.BreakOnNonRetryable(r, resp, err) {
				return nil, err
			}
			if err != nil {
				l.Warnf("%s (%s)", err, r)
			}
			return token, err
		})
		if err != nil {
			if len(cfg.Audience) > 0 {
				err = fmt.Errorf("could not obtain OIDC token for audience %s: %w", cfg.Audience, err)
			} else {
				err = fmt.Errorf("could not obtain OIDC token for default audience: %w", err)
			}
			if oidcTokenRefused(err) {
				return NewExitError(OIDCTokenRefusedExitStatus, err)
			}
			return err
		}

		if !cfg.SkipRedaction {
			jobClient, err := jobapi.NewDefaultClient(ctx)
			if err != nil {
				err = fmt.Errorf("the Job API client could not be created (error: %w)", err)
				if errors.Is(err, jobapi.ErrJobAPIUnavailable) {
					err = fmt.Errorf("the Job API is unavailable on this machine, as it requires Unix domain sockets to function. On Windows, Unix domain sockets require Windows build 17063 or newer (Windows 10 version 1803 / Windows Server 2019 onwards)")
				}
				return fmt.Errorf(
					"automatic OIDC token redaction requires the Job API, but %w. The command failed instead of outputting the token because it could leak in logs without redaction. To output the token anyway, explicitly opt out of redaction with --skip-redaction or BUILDKITE_AGENT_OIDC_REQUEST_TOKEN_SKIP_TOKEN_REDACTION=true",
					err,
				)
			}

			if err := AddToRedactor(ctx, l, jobClient, token.Token); err != nil {
				if cfg.Debug {
					return err
				}
				return errOIDCRedact
			}
		}

		switch cfg.Format {
		case "jwt":
			_, _ = fmt.Fprintln(c.Root().Writer, token.Token)

		case "gcp":
			type gcpOIDCTokenResponse struct {
				IDToken   string `json:"id_token"`
				TokenType string `json:"token_type"`
				Version   int    `json:"version"`
				Success   bool   `json:"success"`
			}

			jsonOutput, err := json.Marshal(gcpOIDCTokenResponse{
				IDToken:   token.Token,
				TokenType: "urn:ietf:params:oauth:token-type:jwt",
				Version:   1,
				Success:   true,
			})
			if err != nil {
				return fmt.Errorf("failed to marshal GCP response: %w", err)
			}

			_, _ = fmt.Fprintln(c.Root().Writer, string(jsonOutput))

		default:
			// This should never happen because we validate the format earlier
			return fmt.Errorf("unknown format %q", cfg.Format)
		}

		return nil
	},
}

// oidcTokenRefused reports whether err represents the Buildkite API positively
// refusing to issue an OIDC token: an API response status that indicates
// retrying will not succeed. Only statuses that definitively represent a
// refusal are listed; every other status — including timeout-like or
// connection-scoped 4xx (408, 421, 425, 429) and anything nonstandard from an
// intermediary — is treated as transient, because a spurious transient
// classification merely costs the caller a retry, while a spurious refusal
// makes it abandon a credential it could have obtained.
func oidcTokenRefused(err error) bool {
	var errResp *api.ErrorResponse
	if !errors.As(err, &errResp) || errResp.Response == nil {
		return false
	}
	switch errResp.Response.StatusCode {
	case http.StatusBadRequest, // 400
		http.StatusUnauthorized,        // 401
		http.StatusForbidden,           // 403
		http.StatusNotFound,            // 404
		http.StatusGone,                // 410
		http.StatusUnprocessableEntity: // 422
		return true
	default:
		return false
	}
}
