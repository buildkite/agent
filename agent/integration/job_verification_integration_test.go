package integration

import (
	"context"
	"fmt"
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
				"DEPLOY": "1", // overridden by pipeline env
			},
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
			"BUILDKITE_PLUGINS": `[{"github.com/buildkite-plugins/some-buildkite-plugin#v1.0.0":{"key":"value"}}]`,
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
)

func TestJobVerification(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name                     string
		agentConf                agent.AgentConfiguration
		job                      api.Job
		signingKey               jwk.Key
		verificationJWKS         jwk.Set
		mockBootstrapExpectation func(*bintest.Mock)
		expectedExitStatus       string
		expectedSignalReason     string
		expectLogsContain        []string
	}{
		{
			name:                     "when job signature is invalid, and InvalidSignatureBehavior is block, it refuses the job",
			agentConf:                agent.AgentConfiguration{JobVerificationInvalidSignatureBehavior: agent.VerificationBehaviourBlock},
			job:                      job,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas), // different signing and verification keys
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyAlpacas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain:        []string{"⚠️ ERROR"},
		},
		{
			name:                     "when job signature is invalid, and InvalidSignatureBehavior is warn, it warns and runs the job",
			agentConf:                agent.AgentConfiguration{JobVerificationInvalidSignatureBehavior: agent.VerificationBehaviourWarn},
			job:                      job,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas), // different signing and verification keys
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyAlpacas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectLogsContain:        []string{"⚠️ WARNING"},
		},
		{
			name:                     "when job signature is valid, it runs the job",
			agentConf:                agent.AgentConfiguration{JobVerificationInvalidSignatureBehavior: agent.VerificationBehaviourBlock},
			job:                      job,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when job signature is missing, and NoSignatureBehavior is block, it refuses the job",
			agentConf:                agent.AgentConfiguration{JobVerificationNoSignatureBehavior: agent.VerificationBehaviourBlock},
			job:                      job,
			signingKey:               nil,
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, "this one is the naughty one")),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain:        []string{"⚠️ ERROR"},
		},
		{
			name:                     "when job signature is missing, and NoSignatureBehavior is warn, it warns and runs the job",
			agentConf:                agent.AgentConfiguration{JobVerificationNoSignatureBehavior: agent.VerificationBehaviourWarn},
			job:                      job,
			signingKey:               nil,
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectLogsContain:        []string{"⚠️ WARNING"},
		},
		{
			name:                     "when the step signature matches, but the job doesn't match the step, it fails signature verification",
			agentConf:                agent.AgentConfiguration{JobVerificationNoSignatureBehavior: agent.VerificationBehaviourBlock},
			job:                      jobWithMismatchedStepAndJob,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"⚠️ ERROR",
				fmt.Sprintf(`the value of field "command" on the job (%q) does not match the value of the field on the step (%q)`,
					jobWithMismatchedStepAndJob.Env["BUILDKITE_COMMAND"], jobWithMismatchedStepAndJob.Step.Command),
			},
		},
		{
			name:                     "when the step signature matches, but the plugins doesn't match the step, it fails signature verification",
			agentConf:                agent.AgentConfiguration{JobVerificationNoSignatureBehavior: agent.VerificationBehaviourBlock},
			job:                      jobWithMismatchedPlugins,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"⚠️ ERROR",
				fmt.Sprintf(`the value of field "plugins" on the job (%q) does not match the value of the field on the step (%q)`,
					jobWithMismatchedPlugins.Env["BUILDKITE_PLUGINS"], job.Env["BUILDKITE_PLUGINS"]),
			},
		},
		{
			name:                     "when the step has a signature, but the JobRunner doesn't have a verification key, it fails signature verification",
			agentConf:                agent.AgentConfiguration{},
			job:                      job,
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
			agentConf:                agent.AgentConfiguration{JobVerificationNoSignatureBehavior: agent.VerificationBehaviourBlock},
			job:                      jobWithMismatchedEnv,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain:        []string{"⚠️ ERROR"},
		},
		{
			name:                     "when the step has a signature, but the step env is not in the job env, it fails signature verification",
			agentConf:                agent.AgentConfiguration{JobVerificationNoSignatureBehavior: agent.VerificationBehaviourBlock},
			job:                      jobWithStepEnvButNoCorrespondingJobEnv,
			signingKey:               symmetricJWKFor(t, signingKeyLlamas),
			verificationJWKS:         jwksFromKeys(t, symmetricJWKFor(t, signingKeyLlamas)),
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain:        []string{"⚠️ ERROR"},
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

			tc.job.Step = signStep(t, pipelineUploadEnv, tc.job.Step, tc.signingKey)
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

func signStep(t *testing.T, env map[string]string, step pipeline.CommandStep, key jwk.Key) pipeline.CommandStep {
	t.Helper()

	t.Logf("%s: signing step with key: %v", t.Name(), key)
	if key == nil {
		return step
	}

	signature, err := pipeline.Sign(env, &step, key)
	if err != nil {
		t.Fatalf("signing step: %v", err)
	}
	step.Signature = signature
	return step
}
