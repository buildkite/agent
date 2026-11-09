package clicommand

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/identity"
	"github.com/buildkite/agent/v4/logger"
	"github.com/urfave/cli/v3"
)

type IdentityConfig struct {
	GlobalConfig
	KeyURI string `cli:"key-uri" validate:"required"`
	Create bool   `cli:"create"`
}

// IdentityCommand creates or inspects the public identity used by a provisioner.
var IdentityCommand = &cli.Command{
	Name:   "identity",
	Usage:  "Create or inspect an experimental key-backed agent identity",
	Hidden: true,
	Description: `Print the public JWK and its SHA-256 thumbprint as JSON.
Requires --experiment host-bound-sessions. Use --create for a new local hardware
key, or open an existing AWS KMS key without --create. Have a trusted provisioner
authorize the thumbprint. This command never prints private keys. Existing keys
are not replaced by --create.`,
	Flags: append(globalFlags(),
		&cli.StringFlag{Name: "key-uri", Usage: "Signing provider URI (awskms, mackms, capi or tpmkms)"},
		&cli.BoolFlag{Name: "create", Usage: "Create a local hardware key instead of opening it; not supported for AWS KMS"},
	),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, _, _, done := setupLoggerAndConfig[IdentityConfig](ctx, c)
		defer done()
		if !experiments.IsEnabled(ctx, experiments.HostBoundSessions) {
			return fmt.Errorf("identity requires --experiment host-bound-sessions")
		}
		key, closeKey, err := identity.Open(ctx, cfg.KeyURI, cfg.Create)
		if err != nil {
			return err
		}
		defer closeKey() //nolint:errcheck // Command errors take precedence.
		return json.NewEncoder(c.Root().Writer).Encode(key)
	},
}

func (cfg AgentStartConfig) validateIdentity(ctx context.Context) error {
	if cfg.HostKeyURI == "" && cfg.EnrollmentToken == "" {
		if cfg.Token == "" {
			return fmt.Errorf("a registration token is required")
		}
		return nil
	}
	if !experiments.IsEnabled(ctx, experiments.HostBoundSessions) {
		return fmt.Errorf("host identity requires --experiment host-bound-sessions")
	}
	if cfg.HostKeyURI == "" || !isIndirectToken(cfg.EnrollmentToken) || cfg.Token != "" {
		return fmt.Errorf("host identity requires --host-key-uri and --enrollment-token=file://PATH or fd://N, without --token")
	}
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil {
		return fmt.Errorf("host identity requires an HTTPS agent endpoint without URL credentials")
	}
	if cfg.DebugHTTP {
		return fmt.Errorf("host identity does not support --debug-http, which dumps credentials")
	}
	if cfg.AcquireJob != "" {
		return fmt.Errorf("host identity does not yet support --acquire-job")
	}
	return nil
}

// enrollHost runs once for the process, not once per spawned worker. The grant
// source and signing key URI are never included in AgentConfiguration or job env.
func enrollHost(ctx context.Context, cfg AgentStartConfig, l logger.Logger, conf *api.Config) (func() error, error) {
	key, closeKey, err := identity.Open(ctx, cfg.HostKeyURI, false)
	if err != nil {
		return nil, err
	}
	grant, err := resolveRegistrationToken(cfg.EnrollmentToken)
	if err != nil {
		_ = closeKey()
		return nil, fmt.Errorf("read enrollment grant: %w", err)
	}
	if grant == "" {
		_ = closeKey()
		return nil, fmt.Errorf("enrollment grant is empty")
	}
	conf.Token, conf.HostKey = grant, key
	enrolled, _, err := api.NewClient(l, *conf).Enroll(ctx)
	if err != nil {
		_ = closeKey()
		return nil, fmt.Errorf("enroll host: %w", err)
	}
	conf.Token, conf.TokenType = enrolled.AccessToken, "DPoP"
	return closeKey, nil
}
