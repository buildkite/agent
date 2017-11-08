package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/buildkite/agent/env"
	"github.com/stretchr/testify/assert"
)

func TestPipelineParser(t *testing.T) {
	var err error
	var result interface{}
	var j []byte

	environ := env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`})

	// It parses pipelines with .yml filenames
	result, err = PipelineParser{Filename: "awesome.yml", Pipeline: []byte("steps:\n  - label: \"hello ${ENV_VAR_FRIEND}\""), Env: environ}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `{"steps":[{"label":"hello \"friend\""}]}`, string(j))

	// It parses complex YAML files
	result, err = PipelineParser{Filename: "awesome.yml", Pipeline: []byte(`base_step: &base_step
  type: script
  agent_query_rules:
    - queue=default

steps:
- <<: *base_step
  name: ':docker: building image'
  command: docker build .
  agents:
    queue: default`)}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `{"base_step":{"agent_query_rules":["queue=default"],"type":"script"},"steps":[{"agent_query_rules":["queue=default"],"agents":{"queue":"default"},"command":"docker build .","name":":docker: building image","type":"script"}]}`, string(j))

	// It parses pipelines with .yaml filenames
	result, err = PipelineParser{Filename: "awesome.yaml", Pipeline: []byte("steps:\n  - label: \"hello ${ENV_VAR_FRIEND}\""), Env: environ}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `{"steps":[{"label":"hello \"friend\""}]}`, string(j))

	// Returns YAML parsing errors
	result, err = PipelineParser{Filename: "awesome.yml", Pipeline: []byte("steps: %blah%")}.Parse()
	assert.NotNil(t, err)
	assert.Equal(t, `Failed to parse YAML: found character that cannot start any token`, fmt.Sprintf("%s", err))

	// Returns JSON parsing errors
	result, err = PipelineParser{Filename: "awesome.json", Pipeline: []byte("{"), Env: environ}.Parse()
	assert.NotNil(t, err)
	assert.Equal(t, `Failed to parse JSON: unexpected end of JSON input`, fmt.Sprintf("%s", err))

	// It parses pipelines with .json filenames
	result, err = PipelineParser{Filename: "thing.json", Pipeline: []byte("\n\n     \n  { \"foo\": \"bye ${ENV_VAR_FRIEND}\" }\n"), Env: environ}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `{"foo":"bye \"friend\""}`, string(j))

	// It parses unknown YAML
	result, err = PipelineParser{Pipeline: []byte("steps:\n  - label: \"hello ${ENV_VAR_FRIEND}\""), Env: environ}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `{"steps":[{"label":"hello \"friend\""}]}`, string(j))

	// Returns YAML parsing errors if the content looks like YAML
	result, err = PipelineParser{Pipeline: []byte("steps: %blah%")}.Parse()
	assert.NotNil(t, err)
	assert.Equal(t, `Failed to parse YAML: found character that cannot start any token`, fmt.Sprintf("%s", err))

	// It parses unknown JSON objects
	result, err = PipelineParser{Pipeline: []byte("\n\n     \n  { \"foo\": \"bye ${ENV_VAR_FRIEND}\" }\n"), Env: environ}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `{"foo":"bye \"friend\""}`, string(j))

	// It parses unknown JSON arrays
	result, err = PipelineParser{Pipeline: []byte("\n\n     \n  [ { \"foo\": \"bye ${ENV_VAR_FRIEND}\" } ]\n"), Env: environ}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `[{"foo":"bye \"friend\""}]`, string(j))

	// Returns JSON parsing errors if the content looks like JSON
	result, err = PipelineParser{Pipeline: []byte("{")}.Parse()
	assert.NotNil(t, err)
	assert.Equal(t, `Failed to parse JSON: unexpected end of JSON input`, fmt.Sprintf("%s", err))
}

func TestPipelineParserInterpolatesKeysAsWellAsValues(t *testing.T) {
	var pipeline = `{
		"env": {
			"${FROM_ENV}TEST1": "MyTest",
			"TEST2": "${FROM_ENV}"
		}
	}`

	var decoded struct {
		Env map[string]string `json:"env"`
	}

	environ := env.FromSlice([]string{`FROM_ENV=llamas`})

	result, err := PipelineParser{Pipeline: []byte(pipeline), Env: environ}.Parse()
	if err != nil {
		t.Fatal(err)
	}

	err = decodeIntoStruct(&decoded, result)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, `MyTest`, decoded.Env["llamasTEST1"])
	assert.Equal(t, `llamas`, decoded.Env["TEST2"])
}

func TestPipelineParserLoadsGlobalEnvBlockFirst(t *testing.T) {
	var pipeline = `{
		"env": {
			"TEAM1": "England",
			"TEAM2": "Australia",
			"HEADLINE": "${TEAM1} smashes ${TEAM2} to win the ashes in ${YEAR_FROM_SHELL}!!"
		},
		"steps": [{
			"command": "echo ${HEADLINE}"
		}]
	}`

	var decoded struct {
		Env   map[string]string `json:"env"`
		Steps []struct {
			Command string `json:"command"`
		} `json:"steps"`
	}

	environ := env.FromSlice([]string{`YEAR_FROM_SHELL=1912`})

	result, err := PipelineParser{Pipeline: []byte(pipeline), Env: environ}.Parse()
	if err != nil {
		t.Fatal(err)
	}

	err = decodeIntoStruct(&decoded, result)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, `England`, decoded.Env["TEAM1"])
	assert.Equal(t, `England smashes Australia to win the ashes in 1912!!`, decoded.Env["HEADLINE"])
	assert.Equal(t, `echo England smashes Australia to win the ashes in 1912!!`, decoded.Steps[0].Command)
}

func decodeIntoStruct(into interface{}, from interface{}) error {
	b, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, into)
}

func TestPipelineParserLoadsSystemEnvironment(t *testing.T) {
	var pipeline = `{
		"steps": [{
			"command": "echo ${LLAMAS_ROCK?}"
		}]
	}`

	var decoded struct {
		Steps []struct {
			Command string `json:"command"`
		} `json:"steps"`
	}

	_, err := PipelineParser{Pipeline: []byte(pipeline)}.Parse()
	if err == nil {
		t.Fatalf("Expected $LLAMAS_ROCK: not set")
	}

	os.Setenv("LLAMAS_ROCK", "absolutely")
	defer os.Unsetenv("LLAMAS_ROCK")

	result2, err := PipelineParser{Pipeline: []byte(pipeline)}.Parse()
	if err != nil {
		t.Fatal(err)
	}

	err = decodeIntoStruct(&decoded, result2)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Steps[0].Command != "echo absolutely" {
		t.Fatalf("Unexpected: %q", decoded.Steps[0].Command)
	}
}
