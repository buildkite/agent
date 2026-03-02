package clicommand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/urfave/cli"
)

const gitCredentialsHelperHelpDescription = `Usage:

    buildkite-agent git-credential-helper [options...]

Description:

Ask buildkite for credentials to use to authenticate with Github when cloning via HTTPS.
The credentials are returned in the git-credential format.

This command will only work if the organization running the job has connected a Github app with Code Access enabled, and
if the pipeline has this feature enabled. All hosted compute jobs automatically qualify for this feature.

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

var GitCredentialsHelperCommand = cli.Command{
	Name:        "git-credentials-helper",
	Usage:       "Internal process used by hosted compute jobs to authenticate with Github",
	Category:    categoryInternal,
	Description: gitCredentialsHelperHelpDescription,
	Flags: append(globalFlags(),
		cli.StringFlag{
			Name:   "job-id",
			Usage:  "The job id to get credentials for",
			EnvVar: "BUILDKITE_JOB_ID",
		},

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		// DebugHTTPFlag, // Not present due to the possibility of leaking code access tokens to logs
	),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[GitCredentialsHelperConfig](ctx, c)
		defer done()

		l.Debug("Action: %s", cfg.Action)
		if cfg.Action != "get" {
			// other actions are store and erase, which we don't support
			// see: https://git-scm.com/docs/gitcredentials#Documentation/gitcredentials.txt-codegetcode
			return nil
		}

		// ie, if the flags are from the command line rather than from the environment, which is how they should be passed
		// to this process when it's called through the job executor
		if os.Getenv("BUILDKITE_JOB_ID") == "" {
			l.Warn("📎💬 It looks like you're calling this command directly in a step, rather than having the agent automatically call it")
			l.Warn("This command is intended to be used as a git credential helper, and not called directly.")
		}

		// git passes the details of the current clone process to the credential helper via stdin
		// we need to parse this to get the repo URL
		// see: https://git-scm.com/docs/git-credential
		stdin, err := io.ReadAll(os.Stdin)
		if err != nil {
			return handleAuthError(c, l, fmt.Errorf("failed to read stdin: %w", err))
		}

		l.Debug("Git credential input:\n%s\n", string(stdin))

		l.Debug("Authenticating checkout using Buildkite Github App Credentials...")

		repo, err := parseGitURLFromCredentialInput(string(stdin))
		if err != nil {
			return handleAuthError(c, l, fmt.Errorf("failed to parse git URL from stdin: %w", err))
		}

		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))
		tok, _, err := client.GenerateGithubCodeAccessToken(ctx, repo, cfg.JobID)
		if err != nil {
			return handleAuthError(c, l, fmt.Errorf("failed to get github app credentials: %w", err))
		}

		fmt.Fprintln(c.App.Writer, "username=token")  //nolint:errcheck // CLI output; errors are non-actionable
		fmt.Fprintln(c.App.Writer, "password="+tok) //nolint:errcheck // CLI output; errors are non-actionable
		fmt.Fprintln(c.App.Writer, "")              //nolint:errcheck // CLI output; errors are non-actionable

		l.Debug("Authentication successful!")

		return nil
	},
}

// handleAuthError is a helper function that logs an error and outputs a dummy password
// git continues with clones etc even when the credential helper fails, so we should output something that will 100% cause
// the clone to fail
// this function always returns a cli.ExitError
func handleAuthError(c *cli.Context, l logger.Logger, err error) error {
	l.Error("Error: %v. Authentication will proceed, but will fail.", err)
	fmt.Fprintln(c.App.Writer, "username=fail") //nolint:errcheck // CLI output; errors are non-actionable
	fmt.Fprintln(c.App.Writer, "password=fail") //nolint:errcheck // CLI output; errors are non-actionable
	fmt.Fprintln(c.App.Writer, "")              //nolint:errcheck // CLI output; errors are non-actionable

	return cli.NewExitError("", 1)
}

var (
	errMissingComponent = errors.New("missing component in git credential input")
	errNotHTTPS         = errors.New("git remote must be using the https protocol to use Github App authentication")
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
