package integration

import (
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/bintest/v3"
	"github.com/buildkite/go-pipeline"
	"github.com/buildkite/go-pipeline/signature"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

const (
	defaultJobID         = "my-job-id"
	defaultRepositoryURL = "git@github.com:buildkite/agent.git"
	signingKeyLlamas     = "llamas"
	signingKeyAlpacas    = "alpacas"
)

var (
	pipelineUploadEnv = map[string]string{
		"DEPLOY": "0",
	}

	job = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
			Plugins: pipeline.Plugins{{
				Source: "some#v1.0.0",
				Config: map[string]string{
					"key": "value",
				},
			}},
			Env: map[string]string{
				"DEPLOY": "1",
			},
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_PLUGINS": `[{"github.com/buildkite-plugins/some-buildkite-plugin#v1.0.0":{"key":"value"}}]`,
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "1", // step env overrides pipeline env
		},
		Token: "bkaj_job-token",
	}

	jobWithNoPluginConfig = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
			Plugins: pipeline.Plugins{{
				Source: "some#v1.0.0",
				Config: nil,
			}},
			Env: map[string]string{
				"DEPLOY": "1",
			},
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_PLUGINS": `[{"github.com/buildkite-plugins/some-buildkite-plugin#v1.0.0":null}]`,
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "1", // step env overrides pipeline env
		},
		Token: "bkaj_job-token",
	}

	jobWithNoPlugins = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
			Plugins: nil,
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_PLUGINS": "",
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
		},
		Token: "bkaj_job-token",
	}

	jobWithNullPlugins = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
			Plugins: nil,
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_PLUGINS": "null",
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
		},
		Token: "bkaj_job-token",
	}

	jobWithMismatchedStepAndJob = api.Job{
		ID:                 defaultJobID,
		ChunksMaxSizeBytes: 1024,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo 'THIS ISN'T HELLO WORLD!!!! CRIMES'",
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
		},
		Token: "bkaj_job-token",
	}

	jobWithMismatchedPlugins = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
			Plugins: pipeline.Plugins{{
				Source: "some#v1.0.0",
				Config: map[string]string{
					"key": "value",
				},
			}},
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_PLUGINS": `[{"github.com/buildkite-plugins/crimes-buildkite-plugin#v1.0.0":{"steal":"everything"}}]`,
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
		},
		Token: "bkaj_job-token",
	}

	jobWithMissingPlugins = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
			Plugins: pipeline.Plugins{{
				Source: "some#v1.0.0",
				Config: map[string]string{
					"key": "value",
				},
			}},
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_PLUGINS": "null",
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
		},
		Token: "bkaj_job-token",
	}

	jobWithMismatchedEnv = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "crimes",
		},
		Token: "bkaj_job-token",
	}

	jobWithStepEnvButNoCorrespondingJobEnv = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
			Env:     map[string]string{"CRIMES": "disable"},
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
		},
		Token: "bkaj_job-token",
	}

	jobWithPipelineEnvButNoCorrespondingJobEnv = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_REPO":    defaultRepositoryURL,
		},
		Token: "bkaj_job-token",
	}

	jobWithMatrix = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step:               stepWithMatrix(),
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_PLUGINS": `[{"github.com/buildkite-plugins/some-buildkite-plugin#v1.0.0":{"message":"hello world"}}]`,
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
			"MESSAGE":           "hello world",
		},
		MatrixPermutation: pipeline.MatrixPermutation{
			"greeting": "hello",
			"object":   "world",
		},
		Token: "bkaj_job-token",
	}

	jobWithInvalidMatrixPermutation = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step:               stepWithMatrix(),
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo goodbye mister anderson",
			"BUILDKITE_PLUGINS": `[{"github.com/buildkite-plugins/some-buildkite-plugin#v1.0.0":{"message":"goodbye mister anderson"}}]`,
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
			"MESSAGE":           "goodbye mister anderson",
		},
		MatrixPermutation: pipeline.MatrixPermutation{ // crimes here:
			"greeting": "goodbye",
			"object":   "mister anderson",
		},
		Token: "bkaj_job-token",
	}

	jobWithMatrixMismatch = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step:               stepWithMatrix(),
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_PLUGINS": `[{"github.com/buildkite-plugins/some-buildkite-plugin#v1.0.0":{"message":"hello world"}}]`,
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
			"MESSAGE":           "goodbye mister anderson", // crimes~!
		},
		MatrixPermutation: pipeline.MatrixPermutation{
			"greeting": "hello",
			"object":   "world",
		},
		Token: "bkaj_job-token",
	}

	jobWithPipelineInvariantsInEnv = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
		},
		Token: "bkaj_job-token",
	}

	jobWithInvalidPipelineInvariantsInEnv = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_REPO":    "https://github.com/haxors/agent.git",
			"DEPLOY":            "0",
		},
		Token: "bkaj_job-token",
	}

	jobWithSecrets = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello secrets",
			Secrets: pipeline.Secrets{
				pipeline.Secret{Key: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"},
				pipeline.Secret{Key: "API_TOKEN", EnvironmentVariable: "API_TOKEN"},
			},
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello secrets",
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
		},
		Token: "bkaj_job-token",
	}

	jobWithSecretsCustomEnv = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello custom secrets",
			Secrets: pipeline.Secrets{
				pipeline.Secret{Key: "DATABASE_URL", EnvironmentVariable: "DB_CONNECTION_STRING"},
				pipeline.Secret{Key: "API_TOKEN", EnvironmentVariable: "CUSTOM_API_KEY"},
			},
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello custom secrets",
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
		},
		Token: "bkaj_job-token",
	}

	jobWithEmptySecrets = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello no secrets",
			Secrets: pipeline.Secrets{},
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello no secrets",
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
		},
		Token: "bkaj_job-token",
	}

	jobWithNilSecrets = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello no secrets",
			Secrets: nil,
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello no secrets",
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
		},
		Token: "bkaj_job-token",
	}

	jobWithMismatchedSecrets = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
			Secrets: pipeline.Secrets{
				pipeline.Secret{Key: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"},
			},
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_REPO":    defaultRepositoryURL,
			"DEPLOY":            "0",
		},
		Token: "bkaj_job-token",
	}
)

func TestJobVerification(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	cases := []struct {
		name                     string
		agentConf                agent.AgentConfiguration
		job                      api.Job
		repositoryURL            string
		signingKey               jwk.Key
		verificationJWKS         jwk.Set
		mockBootstrapExpectation func(*bintest.Mock)
		expectedExitStatus       string
		expectedSignalReason     string
		expectLogsContain        []string
	}{
		{
			name:                     "when job signature is invalid, and JobVerificationFailureBehaviour is block, it refuses the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      job,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas), // different signing and verification keys
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyAlpacas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain:        []string{"+++ ⛔"},
		},
		{
			name:                     "when job signature is invalid, and JobVerificationFailureBehaviour is warn, it warns and runs the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourWarn},
			job:                      job,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas), // different signing and verification keys
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyAlpacas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectLogsContain:        []string{"+++ ⚠️"},
		},
		{
			name:                     "when job signature is valid, it runs the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      job,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when job signature is valid and there are no plugins, it runs the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithNoPlugins,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when job signature is valid and plugins is null, it runs the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithNullPlugins,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when job signature is missing, and JobVerificationFailureBehaviour is block, it refuses the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      job,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               nil,
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, "this one is the naughty one")),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain:        []string{"+++ ⛔"},
		},
		{
			name:                     "when job signature is missing, and JobVerificationFailureBehaviour is warn, it warns and runs the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourWarn},
			job:                      job,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               nil,
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectLogsContain:        []string{"+++ ⚠️"},
		},
		{
			name:                     "when the step signature matches, but the job doesn't match the step, it fails signature verification",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithMismatchedStepAndJob,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"+++ ⛔",
				"job does not match signed step",
			},
		},
		{
			name:                     "when the step signature matches, and the plugins have null config",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithNoPluginConfig,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
			expectLogsContain:        []string{"✅"},
		},
		{
			name:                     "when the step signature matches, but the plugins are missing from the job, it fails signature verification",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithMissingPlugins,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"+++ ⛔",
				"job does not match signed step",
			},
		},
		{
			name:                     "when the step signature matches, but the plugins doesn't match the step, it fails signature verification",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithMismatchedPlugins,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"+++ ⛔",
				"job does not match signed step",
			},
		},
		{
			name:                     "when the step has a signature, but the JobRunner doesn't have a verification key, it fails signature verification",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      job,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         nil,
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"+++ ⛔",
				"cannot verify signature. JWK for pipeline verification is not configured",
			},
		},
		{
			name:                     "when the step has a signature, but the JobRunner doesn't have a verification key, and JobVerificationFailureBehaviour is warn, it warns and runs the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourWarn},
			job:                      job,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         nil,
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
			expectLogsContain: []string{
				"+++ ⛔",
				"cannot verify signature. JWK for pipeline verification is not configured",
			},
		},
		{
			name:                     "when the step has a signature, but the env does not match, it fails signature verification",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithMismatchedEnv,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"+++ ⛔",
				"signature verification failed",
			},
		},
		{
			name:                     "when the step has a signature, but the step env is not in the job env, it fails signature verification",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithStepEnvButNoCorrespondingJobEnv,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"+++ ⛔",
				"job does not match signed step",
			},
		},
		{
			name:                     "when the step has a signature, but the pipeline env is not in the job env, it fails signature verification",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithPipelineEnvButNoCorrespondingJobEnv,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"+++ ⛔",
				"signature verification failed",
			},
		},
		{
			name:                     "when job signature is valid, and there is a valid matrix permutation, it runs the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithMatrix,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when the step signature matches, but the matrix permutation isn't valid, it fails signature verification",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithInvalidMatrixPermutation,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"+++ ⛔",
				"job does not match signed step",
			},
		},
		{
			name:                     "when the step signature matches, but the post-matrix env doesn't match, it fails signature verification",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithMatrixMismatch,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"+++ ⛔",
				"job does not match signed step",
			},
		},
		{
			name:                     "when job has pipeline invariants and the sigature is valid, it runs the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithPipelineInvariantsInEnv,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when job has invalid pipeline invariants, it fails the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithInvalidPipelineInvariantsInEnv,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"+++ ⛔",
				"signature verification failed",
			},
		},
		{
			name:                     "when job signature is valid and has secrets, it runs the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithSecrets,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when job signature is valid and has no secrets, it runs the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithNilSecrets,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when job signature is valid and has secrets with custom environment variables, it runs the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithSecretsCustomEnv,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when job signature is valid and has empty secrets, it runs the job",
			agentConf:                agent.AgentConfiguration{VerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithEmptySecrets,
			repositoryURL:            defaultRepositoryURL,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// create a mock agent API
			e := createTestAgentEndpoint()
			server := e.server()
			defer server.Close()

			mb := mockBootstrap(t)
			tc.mockBootstrapExpectation(mb)
			defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

			t.Logf("%s: signing step with key: %v", t.Name(), tc.signingKey)
			if tc.signingKey != nil {
				err := signature.SignSteps(
					ctx,
					pipeline.Steps{&tc.job.Step},
					tc.signingKey,
					tc.repositoryURL,
					signature.WithEnv(pipelineUploadEnv),
				)
				if err != nil {
					t.Fatalf("signing step: %v", err)
				}
			}

			err := runJob(t, ctx, testRunJobConfig{
				job:              &tc.job,
				server:           server,
				agentCfg:         tc.agentConf,
				mockBootstrap:    mb,
				verificationJWKS: tc.verificationJWKS,
			})
			if err != nil {
				t.Fatalf("runJob() error = %v", err)
			}

			job := e.finishesFor(t, tc.job.ID)[0]

			if got, want := job.ExitStatus, tc.expectedExitStatus; got != want {
				t.Errorf("job.ExitStatus = %q, want %q", got, want)
			}

			logs := e.logsFor(t, tc.job.ID)

			for _, want := range tc.expectLogsContain {
				if !strings.Contains(logs, want) {
					t.Errorf("logs = %q, want to contain %q", logs, want)
				}
			}

			if got, want := job.SignalReason, tc.expectedSignalReason; got != want {
				t.Errorf("job.SignalReason = %q, want %q", got, want)
			}
		})
	}
}

func stepWithMatrix() pipeline.CommandStep {
	return pipeline.CommandStep{
		Command: "echo {{matrix.greeting}} {{matrix.object}}",
		Plugins: pipeline.Plugins{{
			Source: "some#v1.0.0",
			Config: map[string]string{
				"message": "{{matrix.greeting}} {{matrix.object}}",
			},
		}},
		Env: map[string]string{
			"MESSAGE": "{{matrix.greeting}} {{matrix.object}}",
		},
		Matrix: &pipeline.Matrix{
			Setup: pipeline.MatrixSetup{
				"greeting": []string{"hello", "今日は"},
				"object":   []string{"world", "47"},
			},
		},
	}
}

func symmetricJWKFor(t *testing.T, payload string) jwk.Key {
	t.Helper()

	key, err := jwk.FromRaw([]byte(payload)) // calling FromRaw on a []byte will always yield a symmetric key
	if err != nil {
		t.Fatalf("creating jwk: %v", err)
	}

	err = key.Set("alg", "HS256")
	if err != nil {
		t.Fatalf("setting alg: %v", err)
	}

	err = key.Set("kid", payload) // please don't make the id the key in real life
	if err != nil {
		t.Fatalf("setting kid: %v", err)
	}

	return key
}

func jwksFromKeys(t *testing.T, jwkes ...jwk.Key) jwk.Set {
	t.Helper()

	set := jwk.NewSet()
	for _, jwk := range jwkes {
		err := set.AddKey(jwk)
		if err != nil {
			t.Fatalf("adding key to set: %v", err)
		}
	}

	return set
}
