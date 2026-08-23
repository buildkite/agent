package clicommand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/buildkite/agent/v4/api"
	"github.com/urfave/cli/v3"
)

const gitCredentialsHelperHelpDescription = `Usage:

    buildkite-agent git-credentials-helper [options...]

Description:

Ask Buildkite for repository credentials when cloning via HTTPS.
The credentials are returned in the git-credential format.

This command is intended to be used as a git credential helper, and not called directly.`

type GitCredentialsHelperConfig struct {
	GlobalConfig

	JobID  string `cli:"job-id" validate:"required"`
	Action string `cli:"arg:0"`

	// API config
	// DebugHTTP bool // Not present due to the possibility of leaking code access tokens to logs
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
}

var GitCredentialsHelperCommand = &cli.Command{
	Name:        "git-credentials-helper",
	Usage:       "Internal process used to authenticate with repository providers",
	Category:    categoryInternal,
	Description: gitCredentialsHelperHelpDescription,
	Flags: append(globalFlags(),
		&cli.StringFlag{
			Name:    "job-id",
			Usage:   "The job id to get credentials for",
			Sources: cli.EnvVars("BUILDKITE_JOB_ID"),
		},

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		// DebugHTTPFlag, // Not present due to the possibility of leaking code access tokens to logs
	),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[GitCredentialsHelperConfig](ctx, c)
		defer done()

		l.DebugContext(ctx, "Git credentials helper action", "action", cfg.Action)
		if cfg.Action != "get" {
			// other actions are store and erase, which we don't support
			// see: https://git-scm.com/docs/gitcredentials#Documentation/gitcredentials.txt-codegetcode
			return nil
		}

		// ie, if the flags are from the command line rather than from the environment, which is how they should be passed
		// to this process when it's called through the job executor
		if os.Getenv("BUILDKITE_JOB_ID") == "" {
			l.WarnContext(ctx, "📎💬 It looks like you're calling this command directly in a step, rather than having the agent automatically call it")
			l.WarnContext(ctx, "This command is intended to be used as a git credential helper, and not called directly.")
		}

		// git passes the details of the current clone process to the credential helper via stdin
		// we need to parse this to get the repo URL
		// see: https://git-scm.com/docs/git-credential
		stdin, err := io.ReadAll(os.Stdin)
		if err != nil {
			return handleAuthError(c, l, fmt.Errorf("failed to read stdin: %w", err))
		}

		l.DebugContext(ctx, "Requesting repository credentials from Buildkite")

		repo, err := parseGitURLFromCredentialInput(string(stdin))
		if err != nil {
			return handleAuthError(c, l, fmt.Errorf("failed to parse git URL from stdin: %w", err))
		}

		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))
		tok, _, err := client.GenerateRepositoryAccessToken(ctx, repo, cfg.JobID)
		if err != nil {
			return handleAuthError(c, l, fmt.Errorf("failed to get repository credentials: %w", err))
		}
		if tok == "" {
			return handleAuthError(c, l, errors.New("repository credential response contained an empty token"))
		}

		_, _ = fmt.Fprintln(c.Root().Writer, "username=token")
		_, _ = fmt.Fprintln(c.Root().Writer, "password="+tok)
		_, _ = fmt.Fprintln(c.Root().Writer, "")

		l.DebugContext(ctx, "Authentication successful!")

		return nil
	},
}

// handleAuthError is a helper function that logs an error and outputs a dummy password
// git continues with clones etc even when the credential helper fails, so we should output something that will 100% cause
// the clone to fail
// this function always returns a cli.ExitError
func handleAuthError(c *cli.Command, l *slog.Logger, err error) error {
	l.Error(fmt.Sprintf("Error: %v. Authentication will proceed, but will fail.", err))
	_, _ = fmt.Fprintln(c.Root().Writer, "username=fail")
	_, _ = fmt.Fprintln(c.Root().Writer, "password=fail")
	_, _ = fmt.Fprintln(c.Root().Writer, "")

	return cli.Exit("", 1)
}

var (
	errMissingComponent = errors.New("missing component in git credential input")
	errNotHTTPS         = errors.New("git remote must use the https protocol for repository credentials")
)

func parseGitURLFromCredentialInput(input string) (string, error) {
	lines := strings.Split(input, "\n")

	components := map[string]string{
		"protocol": "",
		"host":     "",
		"path":     "",
	}
	for _, line := range lines {
		if p, ok := strings.CutPrefix(line, "protocol="); ok {
			components["protocol"] = strings.TrimSpace(p)
		}
		if p, ok := strings.CutPrefix(line, "host="); ok {
			components["host"] = strings.TrimSpace(p)
		}
		if p, ok := strings.CutPrefix(line, "path="); ok {
			components["path"] = strings.TrimSpace(p)
		}
	}

	for k, v := range components {
		if v == "" {
			return "", fmt.Errorf("%w: %s", errMissingComponent, k)
		}
	}

	if components["protocol"] != "https" {
		return "", errNotHTTPS
	}

	u := url.URL{
		Scheme: components["protocol"],
		Host:   components["host"],
		Path:   components["path"],
	}

	return u.String(), nil
}
