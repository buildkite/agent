package secrets

import (
	"context"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/go-pipeline"
)

// Processor interface defines how different types of secrets are processed.
type Processor interface {
	// SupportsSecret returns true if this processor can handle the given secret configuration.
	SupportsSecret(secret *pipeline.Secret) bool

	// ProcessSecret processes the secret with the given value.
	ProcessSecret(ctx context.Context, secret *pipeline.Secret, value string) error
}

// EnvironmentVariableProcessor handles secrets that should be set as environment variables.
type EnvironmentVariableProcessor struct {
	Env       *env.Environment
	Redactors *replacer.Mux
}

// SupportsSecret returns true for secrets with non-empty EnvironmentVariable field.
func (p *EnvironmentVariableProcessor) SupportsSecret(secret *pipeline.Secret) bool {
	return secret.EnvironmentVariable != ""
}

// ProcessSecret sets the secret as an environment variable and registers it for redaction.
func (p *EnvironmentVariableProcessor) ProcessSecret(ctx context.Context, secret *pipeline.Secret, value string) error {
	// Set the environment variable
	p.Env.Set(secret.EnvironmentVariable, value)

	// Register the secret value for redaction immediately
	if p.Redactors != nil {
		p.Redactors.Add(value)
	}

	return nil
}
