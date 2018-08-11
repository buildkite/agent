package agent

import (
	"encoding/json"
	"testing"

	"github.com/buildkite/agent/env"
	"github.com/stretchr/testify/assert"
)

func TestStepSignerSignPipelineIgnoresStepWithoutCommand(t *testing.T) {
	environ := env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`})

	parsed, err := PipelineParser{
		Filename: "awesome.yml",
		Pipeline: []byte("steps:\n  - label: \"hello ${ENV_VAR_FRIEND}\""),
		Env:	  environ}.Parse()

	assert.NoError(t, err)

	signed, err := StepSigner{
		SigningKey: "secret-llama",
	}.SignPipeline(parsed)

	assert.NoError(t, err)

	j, err := json.Marshal(signed)
	assert.Equal(t, `{"steps":[{"label":"hello \"friend\""}]}`, string(j))
}

func TestStepSignerSignPipelineSignsStepWithCommand(t *testing.T) {
	environ := env.FromSlice([]string{`YOUR_NAME="Fred"`})

	parsed, err := PipelineParser{
		Filename: "awesome.yml",
		Pipeline: []byte("steps:\n  - command: \"echo Hello ${YOUR_NAME}\""),
		Env:	  environ}.Parse()

	assert.NoError(t, err)

	signed, err := StepSigner{
		SigningKey: "secret-llama",
	}.SignPipeline(parsed)

	assert.NoError(t, err)

	j, err := json.Marshal(signed)
	assert.Equal(t, `{"steps":[{"command":"echo Hello \"Fred\"","env":{"BUILDKITE_STEP_SIGNATURE":"secret-llamaecho Hello \"Fred\""}}]}`, string(j))
}

func TestStepSignerSignPipelineSignsStepWithCommandAndEnv(t *testing.T) {
	environ := env.FromSlice([]string{`YOUR_NAME="Fred"`})

	parsed, err := PipelineParser{
		Filename: "awesome.yml",
		Pipeline: []byte("steps:\n  - env:\n      EXISTING: \"existing-value\"\n    command: \"echo Hello ${YOUR_NAME}\""),
		Env:	  environ}.Parse()

	assert.NoError(t, err)

	signed, err := StepSigner{
		SigningKey: "secret-llama",
	}.SignPipeline(parsed)

	assert.NoError(t, err)

	j, err := json.Marshal(signed)
	assert.Equal(t, `{"steps":[{"command":"echo Hello \"Fred\"","env":{"BUILDKITE_STEP_SIGNATURE":"secret-llamaecho Hello \"Fred\"","EXISTING":"existing-value"}}]}`, string(j))
}
