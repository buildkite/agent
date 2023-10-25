package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/pipeline"
	"github.com/buildkite/bintest/v3"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

const (
	defaultJobID      = "my-job-id"
	signingKeyLlamas  = "llamas"
	signingKeyAlpacas = "alpacas"
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
			"DEPLOY":            "1", // step env overrides pipeline env
		},
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
			"DEPLOY":            "0",
		},
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
			"DEPLOY":            "0",
		},
	}

	jobWithMismatchedStepAndJob = api.Job{
		ID:                 defaultJobID,
		ChunksMaxSizeBytes: 1024,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo 'THIS ISN'T HELLO WORLD!!!! CRIMES'",
			"DEPLOY":            "0",
		},
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
			"DEPLOY":            "0",
		},
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
			"DEPLOY":            "0",
		},
	}

	jobWithMismatchedEnv = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"DEPLOY":            "crimes",
		},
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
			"DEPLOY":            "0",
		},
	}

	jobWithPipelineEnvButNoCorrespondingJobEnv = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
	}

	jobWithMatrix = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step:               stepWithMatrix(),
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_PLUGINS": `[{"github.com/buildkite-plugins/some-buildkite-plugin#v1.0.0":{"message":"hello world"}}]`,
			"DEPLOY":            "0",
			"MESSAGE":           "hello world",
		},
		MatrixPermutation: pipeline.MatrixPermutation{
			"greeting": "hello",
			"object":   "world",
		},
	}

	jobWithInvalidMatrixPermutation = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step:               stepWithMatrix(),
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo goodbye mister anderson",
			"BUILDKITE_PLUGINS": `[{"github.com/buildkite-plugins/some-buildkite-plugin#v1.0.0":{"message":"goodbye mister anderson"}}]`,
			"DEPLOY":            "0",
			"MESSAGE":           "goodbye mister anderson",
		},
		MatrixPermutation: pipeline.MatrixPermutation{ // crimes here:
			"greeting": "goodbye",
			"object":   "mister anderson",
		},
	}

	jobWithMatrixMismatch = api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step:               stepWithMatrix(),
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_PLUGINS": `[{"github.com/buildkite-plugins/some-buildkite-plugin#v1.0.0":{"message":"hello world"}}]`,
			"DEPLOY":            "0",
			"MESSAGE":           "goodbye mister anderson", // crimes~!
		},
		MatrixPermutation: pipeline.MatrixPermutation{
			"greeting": "hello",
			"object":   "world",
		},
	}
)

func TestJobVerification(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name                     string
		agentConf                agent.AgentConfiguration
		job                      api.Job
		pipelineInvariants       pipeline.PipelineInvariants
		signingKey               jwk.Key
		verificationJWKS         jwk.Set
		mockBootstrapExpectation func(*bintest.Mock)
		expectedExitStatus       string
		expectedSignalReason     string
		expectLogsContain        []string
	}{
		{
			name:                     "when job signature is invalid, and JobVerificationFailureBehaviour is block, it refuses the job",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      job,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas), // different signing and verification keys
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyAlpacas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain:        []string{"⚠️ ERROR"},
		},
		{
			name:                     "when job signature is invalid, and JobVerificationFailureBehaviour is warn, it warns and runs the job",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourWarn},
			job:                      job,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas), // different signing and verification keys
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyAlpacas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectLogsContain:        []string{"⚠️ WARNING"},
		},
		{
			name:                     "when job signature is valid, it runs the job",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      job,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when job signature is valid and there are no plugins, it runs the job",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithNoPlugins,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when job signature is valid and plugins is null, it runs the job",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithNullPlugins,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when job signature is missing, and JobVerificationFailureBehaviour is block, it refuses the job",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      job,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               nil,
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, "this one is the naughty one")),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain:        []string{"⚠️ ERROR"},
		},
		{
			name:                     "when job signature is missing, and JobVerificationFailureBehaviour is warn, it warns and runs the job",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourWarn},
			job:                      job,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               nil,
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectLogsContain:        []string{"⚠️ WARNING"},
		},
		{
			name:                     "when the step signature matches, but the job doesn't match the step, it fails signature verification",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithMismatchedStepAndJob,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"⚠️ ERROR",
				"job does not match signed step",
			},
		},
		{
			name:                     "when the step signature matches, but the plugins are missing from the job, it fails signature verification",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithMissingPlugins,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"⚠️ ERROR",
				"job does not match signed step",
			},
		},
		{
			name:                     "when the step signature matches, but the plugins doesn't match the step, it fails signature verification",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithMismatchedPlugins,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"⚠️ ERROR",
				"job does not match signed step",
			},
		},
		{
			name:                     "when the step has a signature, but the JobRunner doesn't have a verification key, it fails signature verification",
			agentConf:                agent.AgentConfiguration{},
			job:                      job,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         nil,
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"⚠️ ERROR",
				"but no verification key was provided, so the job can't be verified",
			},
		},
		{
			name:                     "when the step has a signature, but the env does not match, it fails signature verification",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithMismatchedEnv,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"⚠️ ERROR",
				"signature verification failed",
			},
		},
		{
			name:                     "when the step has a signature, but the step env is not in the job env, it fails signature verification",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithStepEnvButNoCorrespondingJobEnv,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"⚠️ ERROR",
				"job does not match signed step",
			},
		},
		{
			name:                     "when the step has a signature, but the pipeline env is not in the job env, it fails signature verification",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithPipelineEnvButNoCorrespondingJobEnv,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"⚠️ ERROR",
				"signature verification failed",
			},
		},
		{
			name:                     "when job signature is valid, and there is a valid matrix permutation, it runs the job",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithMatrix,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when the step signature matches, but the matrix permutation isn't valid, it fails signature verification",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithInvalidMatrixPermutation,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"⚠️ ERROR",
				"job does not match signed step",
			},
		},
		{
			name:                     "when the step signature matches, but the post-matrix env doesn't match, it fails signature verification",
			agentConf:                agent.AgentConfiguration{JobVerificationFailureBehaviour: agent.VerificationBehaviourBlock},
			job:                      jobWithMatrixMismatch,
			pipelineInvariants:       pipeline.PipelineInvariants{},
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"⚠️ ERROR",
				"job does not match signed step",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// create a mock agent API
			e := createTestAgentEndpoint()
			server := e.server(tc.job.ID)
			defer server.Close()

			mb := mockBootstrap(t)
			tc.mockBootstrapExpectation(mb)
			defer mb.CheckAndClose(t)

			stepWithInvariants := pipeline.CommandStepWithPipelineInvariants{
				CommandStep:        tc.job.Step,
				PipelineInvariants: tc.pipelineInvariants,
			}

			tc.job.Step = signStep(t, pipelineUploadEnv, stepWithInvariants, tc.signingKey)
			runJob(t, ctx, testRunJobConfig{
				job:              &tc.job,
				server:           server,
				agentCfg:         tc.agentConf,
				mockBootstrap:    mb,
				verificationJWKS: tc.verificationJWKS,
			})

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
	set := jwk.NewSet()
	for _, jwk := range jwkes {
		err := set.AddKey(jwk)
		if err != nil {
			t.Fatalf("adding key to set: %v", err)
		}
	}

	return set
}

func signStep(
	t *testing.T,
	env map[string]string,
	stepWithInvariants pipeline.CommandStepWithPipelineInvariants,
	key jwk.Key,
) pipeline.CommandStep {
	t.Helper()

	t.Logf("%s: signing step with key: %v", t.Name(), key)
	if key == nil {
		return stepWithInvariants.CommandStep
	}

	signature, err := pipeline.Sign(env, &stepWithInvariants, key)
	if err != nil {
		t.Fatalf("signing step: %v", err)
	}
	stepWithInvariants.CommandStep.Signature = signature
	return stepWithInvariants.CommandStep
}
