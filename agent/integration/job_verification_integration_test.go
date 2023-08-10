package integration

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/pipeline"
	"github.com/buildkite/bintest/v3"
)

const (
	defaultJobID = "my-job-id"
	signingKey   = "llamasrock"
)

var (
	jobWithInvalidSignature = &api.Job{
		ID:                 defaultJobID,
		ChunksMaxSizeBytes: 1024,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
			Signature: &pipeline.Signature{
				Algorithm:    "hmac-sha256",
				SignedFields: []string{"command"},
				Value:        "bm90LXRoZS1yZWFsLXNpZ25hdHVyZQ==", // base64("not-the-real-signature"), an invalid signature
			},
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
	}

	jobWithValidSignature = &api.Job{
		ID:                 defaultJobID,
		ChunksMaxSizeBytes: 1024,
		Step: pipeline.CommandStep{
			Command: "echo hello world",
			Signature: &pipeline.Signature{
				Algorithm:    "hmac-sha256",
				SignedFields: []string{"command"},
				// To calculate the below value:
				// $ echo -n "llamasrock" > ~/secretkey.txt &&  \
				//   echo "steps: [{command: 'echo hello world'}]" \
				//   | buildkite-agent pipeline upload --dry-run --signing-key-path ~/secretkey.txt \
				//   | jq ".steps[0].signature.value"
				Value: "lBpQXxY9mrqN4mnhhNXdXr7PfAjXSPG7nN0zoAPclG4=",
			},
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
	}

	jobWithNoSignature = &api.Job{
		ChunksMaxSizeBytes: 1024,
		ID:                 defaultJobID,
		Step:               pipeline.CommandStep{Command: "echo hello world"},
		Env:                map[string]string{"BUILDKITE_COMMAND": "echo hello world"},
	}

	jobWithMismatchedStepAndJob = &api.Job{
		ID:                 defaultJobID,
		ChunksMaxSizeBytes: 1024,
		Step: pipeline.CommandStep{
			Signature: &pipeline.Signature{
				Algorithm:    "hmac-sha256",
				SignedFields: []string{"command"},
				// To calculate the below value:
				// $ echo -n "llamasrock" > ~/secretkey.txt &&  \
				//   echo "steps: [{command: 'echo hello world'}]" \
				//   | buildkite-agent pipeline upload --dry-run --signing-key-path ~/secretkey.txt \
				//   | jq ".steps[0].signature.value"
				Value: "lBpQXxY9mrqN4mnhhNXdXr7PfAjXSPG7nN0zoAPclG4=",
			},
			Command: "echo hello world",
		},
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo 'THIS ISN'T HELLO WORLD!!!! CRIMES'",
		},
	}
)

func TestJobVerification(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                     string
		agentConf                agent.AgentConfiguration
		job                      *api.Job
		mockBootstrapExpectation func(*bintest.Mock)
		expectedExitStatus       string
		expectedSignalReason     string
		expectLogsContain        []string
	}{
		{
			name:                     "when job signature is invalid, and InvalidSignatureBehavior is block, it refuses the job",
			agentConf:                agent.AgentConfiguration{JobVerificationInvalidSignatureBehavior: agent.VerificationBehaviourBlock},
			job:                      jobWithInvalidSignature,
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain:        []string{"⚠️ ERROR"},
		},
		{
			name:                     "when job signature is invalid, and InvalidSignatureBehavior is warn, it warns and runs the job",
			agentConf:                agent.AgentConfiguration{JobVerificationInvalidSignatureBehavior: agent.VerificationBehaviourWarn},
			job:                      jobWithInvalidSignature,
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectLogsContain:        []string{"⚠️ WARNING"},
		},
		{
			name:                     "when job signature is valid, it runs the job",
			agentConf:                agent.AgentConfiguration{JobVerificationInvalidSignatureBehavior: agent.VerificationBehaviourBlock},
			job:                      jobWithValidSignature,
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectedSignalReason:     "",
		},
		{
			name:                     "when job signature is missing, and NoSignatureBehavior is block, it refuses the job",
			agentConf:                agent.AgentConfiguration{JobVerificationNoSignatureBehavior: agent.VerificationBehaviourBlock},
			job:                      jobWithNoSignature,
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain:        []string{"⚠️ ERROR"},
		},
		{
			name:                     "when job signature is missing, and NoSignatureBehavior is warn, it warns and runs the job",
			agentConf:                agent.AgentConfiguration{JobVerificationNoSignatureBehavior: agent.VerificationBehaviourWarn},
			job:                      jobWithNoSignature,
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().Once().AndExitWith(0) },
			expectedExitStatus:       "0",
			expectLogsContain:        []string{"⚠️ WARNING"},
		},
		{
			name:                     "when the step signature matches, but the job doesn't match the step, it fails signature verification",
			agentConf:                agent.AgentConfiguration{JobVerificationNoSignatureBehavior: agent.VerificationBehaviourBlock},
			job:                      jobWithMismatchedStepAndJob,
			mockBootstrapExpectation: func(bt *bintest.Mock) { bt.Expect().NotCalled() },
			expectedExitStatus:       "-1",
			expectedSignalReason:     agent.SignalReasonSignatureRejected,
			expectLogsContain: []string{
				"⚠️ ERROR",
				fmt.Sprintf(`the value of field "command" on the job (%q) does not match the value of the field on the step (%q)`,
					jobWithMismatchedStepAndJob.Env["BUILDKITE_COMMAND"], jobWithMismatchedStepAndJob.Step.Command),
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			keyFile, err := os.CreateTemp("", "keyfile")
			if err != nil {
				t.Fatalf("making keyfile: %v", err)
			}
			defer os.Remove(keyFile.Name())

			_, err = keyFile.Write([]byte(signingKey))
			if err != nil {
				t.Fatalf("writing keyfile: %v", err)
			}

			tc.agentConf.JobVerificationKeyPath = keyFile.Name()

			// create a mock agent API
			e := createTestAgentEndpoint()
			server := e.server(tc.job.ID)
			defer server.Close()

			mb := mockBootstrap(t)
			tc.mockBootstrapExpectation(mb)
			defer mb.CheckAndClose(t)

			runJob(t, tc.job, server, tc.agentConf, mb)

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

func TestWhenTheJobHasASignature_ButTheJobRunnerCantVerify_ItRefusesTheJob(t *testing.T) {
	t.Parallel()

	job := jobWithValidSignature

	// create a mock agent API
	e := createTestAgentEndpoint()
	server := e.server(job.ID)
	defer server.Close()

	mb := mockBootstrap(t)
	mb.Expect().NotCalled()
	defer mb.CheckAndClose(t)

	runJob(t, job, server, agent.AgentConfiguration{}, mb) // note no verification config

	finish := e.finishesFor(t, job.ID)[0]

	if got, want := finish.ExitStatus, "-1"; got != want {
		t.Errorf("job.ExitStatus = %q, want %q", got, want)
	}

	logs := e.logsFor(t, job.ID)

	expectLogsContain := []string{
		"⚠️ ERROR",
		fmt.Sprintf("job %q was signed with signature %q, but no verification key was provided, so the job can't be verified", job.ID, job.Step.Signature.Value),
	}

	for _, want := range expectLogsContain {
		if !strings.Contains(logs, want) {
			t.Errorf("logs = %q, want to contain %q", logs, want)
		}
	}

	if got, want := finish.SignalReason, agent.SignalReasonSignatureRejected; got != want {
		t.Errorf("job.SignalReason = %q, want %q", got, want)
	}
}
