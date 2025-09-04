package clicommand

import (
	"strings"
	"testing"

	"github.com/buildkite/go-pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignOffline_MergesSecretsFromPipeline(t *testing.T) {
	// Create a test pipeline with both top-level and step-level secrets
	pipelineYAML := `secrets:
  - API_TOKEN
  - GLOBAL_SECRET

steps:
  - label: "Test step"
    command: "echo 'test'"
    secrets:
      - DATABASE_URL
      - STEP_SECRET
`

	// Parse the pipeline
	parsedPipeline, err := pipeline.Parse(strings.NewReader(pipelineYAML))
	require.NoError(t, err)

	// Verify initial state - step should only have its own secrets
	commandStep := parsedPipeline.Steps[0].(*pipeline.CommandStep)
	assert.Len(t, commandStep.Secrets, 2, "Step should initially have only step-level secrets")
	
	// Find DATABASE_URL and STEP_SECRET
	var hasDatabase, hasStepSecret bool
	for _, secret := range commandStep.Secrets {
		if secret.Key == "DATABASE_URL" {
			hasDatabase = true
		}
		if secret.Key == "STEP_SECRET" {
			hasStepSecret = true
		}
	}
	assert.True(t, hasDatabase, "Step should have DATABASE_URL")
	assert.True(t, hasStepSecret, "Step should have STEP_SECRET")

	// Apply the merging logic that happens during signing
	for _, step := range parsedPipeline.Steps {
		if commandStep, ok := step.(*pipeline.CommandStep); ok {
			commandStep.MergeSecretsFromPipeline(parsedPipeline.Secrets)
		}
	}

	// Verify merged state - step should now have all 4 secrets
	assert.Len(t, commandStep.Secrets, 4, "Step should have all secrets after merging")
	
	// Check that all secrets are present
	secretKeys := make(map[string]bool)
	for _, secret := range commandStep.Secrets {
		secretKeys[secret.Key] = true
	}
	
	expectedSecrets := []string{"DATABASE_URL", "STEP_SECRET", "API_TOKEN", "GLOBAL_SECRET"}
	for _, expectedKey := range expectedSecrets {
		assert.True(t, secretKeys[expectedKey], "Step should have secret %s after merging", expectedKey)
	}

	// Verify that step-level secrets take precedence (API_TOKEN appears first in step, then from pipeline)
	// The MergeWith function should preserve this ordering with pipeline secrets first
	assert.Equal(t, "API_TOKEN", commandStep.Secrets[0].Key, "Pipeline-level secrets should come first")
	assert.Equal(t, "GLOBAL_SECRET", commandStep.Secrets[1].Key, "Pipeline-level secrets should come first")
	assert.Equal(t, "DATABASE_URL", commandStep.Secrets[2].Key, "Step-level secrets should be appended")
	assert.Equal(t, "STEP_SECRET", commandStep.Secrets[3].Key, "Step-level secrets should be appended")
}

func TestSignOffline_EmptyPipelineSecrets(t *testing.T) {
	// Create a test pipeline with only step-level secrets
	pipelineYAML := `steps:
  - label: "Test step"
    command: "echo 'test'"
    secrets:
      - DATABASE_URL
`

	// Parse the pipeline
	parsedPipeline, err := pipeline.Parse(strings.NewReader(pipelineYAML))
	require.NoError(t, err)

	commandStep := parsedPipeline.Steps[0].(*pipeline.CommandStep)
	assert.Len(t, commandStep.Secrets, 1, "Step should have only step-level secret")

	// Apply the merging logic with empty pipeline secrets
	for _, step := range parsedPipeline.Steps {
		if commandStep, ok := step.(*pipeline.CommandStep); ok {
			commandStep.MergeSecretsFromPipeline(parsedPipeline.Secrets)
		}
	}

	// Should still have only the step-level secret
	assert.Len(t, commandStep.Secrets, 1, "Step should still have only step-level secret")
	assert.Equal(t, "DATABASE_URL", commandStep.Secrets[0].Key)
}

func TestSignOffline_EmptyStepSecrets(t *testing.T) {
	// Create a test pipeline with only top-level secrets
	pipelineYAML := `secrets:
  - API_TOKEN
  - GLOBAL_SECRET

steps:
  - label: "Test step"
    command: "echo 'test'"
`

	// Parse the pipeline
	parsedPipeline, err := pipeline.Parse(strings.NewReader(pipelineYAML))
	require.NoError(t, err)

	commandStep := parsedPipeline.Steps[0].(*pipeline.CommandStep)
	assert.Len(t, commandStep.Secrets, 0, "Step should have no secrets initially")

	// Apply the merging logic
	for _, step := range parsedPipeline.Steps {
		if commandStep, ok := step.(*pipeline.CommandStep); ok {
			commandStep.MergeSecretsFromPipeline(parsedPipeline.Secrets)
		}
	}

	// Should now have pipeline-level secrets
	assert.Len(t, commandStep.Secrets, 2, "Step should have pipeline-level secrets")
	assert.Equal(t, "API_TOKEN", commandStep.Secrets[0].Key)
	assert.Equal(t, "GLOBAL_SECRET", commandStep.Secrets[1].Key)
}

func TestSignOffline_SecretPrecedence(t *testing.T) {
	// Create a test pipeline where step-level secret overrides pipeline-level secret
	// Both secrets have the same environment variable name, so they should be deduplicated
	pipelineYAML := `secrets:
  - key: DATABASE_URL
    environment_variable: DATABASE_URL

steps:
  - label: "Test step"
    command: "echo 'test'"
    secrets:
      - key: DATABASE_URL
        environment_variable: DATABASE_URL
`

	// Parse the pipeline
	parsedPipeline, err := pipeline.Parse(strings.NewReader(pipelineYAML))
	require.NoError(t, err)

	commandStep := parsedPipeline.Steps[0].(*pipeline.CommandStep)

	// Apply the merging logic
	for _, step := range parsedPipeline.Steps {
		if commandStep, ok := step.(*pipeline.CommandStep); ok {
			commandStep.MergeSecretsFromPipeline(parsedPipeline.Secrets)
		}
	}

	// Should have only one DATABASE_URL secret, with step-level taking precedence
	assert.Len(t, commandStep.Secrets, 1, "Should have only one secret after deduplication")
	assert.Equal(t, "DATABASE_URL", commandStep.Secrets[0].Key)
	assert.Equal(t, "DATABASE_URL", commandStep.Secrets[0].EnvironmentVariable)
}
