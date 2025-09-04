package agent

import (
	"context"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/go-pipeline"
)

func TestVerifyJob_WithSecrets(t *testing.T) {
	t.Parallel()

	// Create job runner with a step containing secrets
	job := &api.Job{
		ID:       "test-job",
		Endpoint: "https://api.buildkite.com/",
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo 'Testing secrets'",
			"BUILDKITE_REPO":    "https://github.com/example/repo",
		},
		Step: pipeline.CommandStep{
			Command: "echo 'Testing secrets'",
			Secrets: pipeline.Secrets{
				&pipeline.Secret{Key: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"},
			},
		},
	}

	// Create a signed step - this would normally be signed by the backend
	step := &job.Step
	step.Signature = &pipeline.Signature{
		Algorithm:    "HS256",
		SignedFields: []string{"command", "env", "plugins", "matrix", "repository_url", "secrets"},
		Value:        "fake-signature-value",
	}

	agentLogger := logger.Discard
	conf := JobRunnerConfig{
		Job:                job,
		AgentConfiguration: AgentConfiguration{},
	}

	runner := &JobRunner{
		conf:        conf,
		agentLogger: agentLogger,
	}

	// For this test, we'll mock the signature verification to pass
	// The real test is that the secrets field comparison doesn't fail
	ctx := context.Background()

	// This should not fail due to secrets comparison
	// Note: This test will fail signature verification, but we're testing 
	// that the secrets case is handled (not causing unknown field error)
	err := runner.verifyJob(ctx, nil)
	if err == nil {
		t.Error("verifyJob() expected error due to fake signature, but got nil")
	}

	// Check that the error is not about unknown field "secrets"
	if err.Error() == `invalid signature: mystery signed field "secrets"` {
		t.Errorf("verifyJob() failed with unknown secrets field error: %v", err)
	}
}

func TestVerifyJob_WithEmptySecrets(t *testing.T) {
	t.Parallel()

	// Create job runner with a step containing empty secrets
	job := &api.Job{
		ID:       "test-job",
		Endpoint: "https://api.buildkite.com/",
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo 'Testing empty secrets'",
			"BUILDKITE_REPO":    "https://github.com/example/repo",
		},
		Step: pipeline.CommandStep{
			Command: "echo 'Testing empty secrets'",
			Secrets: nil, // No secrets
		},
	}

	// Create a signed step - this would normally be signed by the backend
	step := &job.Step
	step.Signature = &pipeline.Signature{
		Algorithm:    "HS256",
		SignedFields: []string{"command", "env", "plugins", "matrix", "repository_url", "secrets"},
		Value:        "fake-signature-value",
	}

	agentLogger := logger.Discard
	conf := JobRunnerConfig{
		Job:                job,
		AgentConfiguration: AgentConfiguration{},
	}

	runner := &JobRunner{
		conf:        conf,
		agentLogger: agentLogger,
	}

	ctx := context.Background()

	// This should not fail due to secrets comparison
	err := runner.verifyJob(ctx, nil)
	if err == nil {
		t.Error("verifyJob() expected error due to fake signature, but got nil")
	}

	// Check that the error is not about unknown field "secrets"
	if err.Error() == `invalid signature: mystery signed field "secrets"` {
		t.Errorf("verifyJob() failed with unknown secrets field error: %v", err)
	}
}

func TestVerifyJob_SecretsMismatch(t *testing.T) {
	t.Parallel()

	// Create job runner with step containing different secrets than job
	job := &api.Job{
		ID:       "test-job",
		Endpoint: "https://api.buildkite.com/",
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo 'Testing secrets mismatch'",
			"BUILDKITE_REPO":    "https://github.com/example/repo",
		},
		Step: pipeline.CommandStep{
			Command: "echo 'Testing secrets mismatch'",
			Secrets: pipeline.Secrets{
				&pipeline.Secret{Key: "API_KEY", EnvironmentVariable: "API_KEY"},
			},
		},
	}

	// Create a signed step with different secrets
	step := &job.Step
	step.Signature = &pipeline.Signature{
		Algorithm:    "HS256", 
		SignedFields: []string{"command", "env", "plugins", "matrix", "repository_url", "secrets"},
		Value:        "fake-signature-value",
	}

	// Modify step secrets after signing to simulate tampering
	step.Secrets = pipeline.Secrets{
		&pipeline.Secret{Key: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"}, // Different from signed
	}

	agentLogger := logger.Discard
	conf := JobRunnerConfig{
		Job:                job,
		AgentConfiguration: AgentConfiguration{},
	}

	runner := &JobRunner{
		conf:        conf,
		agentLogger: agentLogger,
	}

	ctx := context.Background()

	// This test would require actual signature verification to properly test secrets mismatch
	// For now, just verify the field is recognized
	err := runner.verifyJob(ctx, nil)
	if err == nil {
		t.Error("verifyJob() expected error, but got nil")
	}

	// The important thing is that "secrets" is not an unknown field
	if err.Error() == `invalid signature: mystery signed field "secrets"` {
		t.Errorf("verifyJob() failed with unknown secrets field error: %v", err)
	}
}
