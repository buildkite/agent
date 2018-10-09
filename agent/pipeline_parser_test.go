package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/agent/env"
	"github.com/stretchr/testify/assert"
)

func TestPipelineParserParsesYaml(t *testing.T) {
	environ := env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`})

	result, err := PipelineParser{
		Filename: "awesome.yml",
		Pipeline: []byte("steps:\n  - label: \"hello ${ENV_VAR_FRIEND}\""),
		Env:      environ}.Parse()

	assert.NoError(t, err)
	j, err := json.Marshal(result)
	assert.Equal(t, `{"steps":[{"label":"hello \"friend\""}]}`, string(j))
}

func TestPipelineParserParsesYamlWithNoInterpolation(t *testing.T) {
	result, err := PipelineParser{
		Filename:        "awesome.yml",
		Pipeline:        []byte("steps:\n  - label: \"hello ${ENV_VAR_FRIEND}\""),
		NoInterpolation: true,
	}.Parse()

	assert.NoError(t, err)
	j, err := json.Marshal(result)
	assert.Equal(t, `{"steps":[{"label":"hello ${ENV_VAR_FRIEND}"}]}`, string(j))
}

func TestPipelineParserSupportsYamlMergesAndAnchors(t *testing.T) {
	complexYAML := `---
base_step: &base_step
  type: script
  agent_query_rules:
    - queue=default

steps:
  - <<: *base_step
    name: ':docker: building image'
    command: docker build .
    agents:
      queue: default`

	result, err := PipelineParser{
		Filename: "awesome.yml",
		Pipeline: []byte(complexYAML)}.Parse()

	assert.NoError(t, err)
	j, err := json.Marshal(result)
	assert.Equal(t, `{"base_step":{"type":"script","agent_query_rules":["queue=default"]},"steps":[{"type":"script","agent_query_rules":["queue=default"],"name":":docker: building image","command":"docker build .","agents":{"queue":"default"}}]}`, string(j))
}

func TestPipelineParserReturnsYamlParsingErrors(t *testing.T) {
	_, err := PipelineParser{Filename: "awesome.yml", Pipeline: []byte("steps: %blah%")}.Parse()
	assert.Error(t, err, `Failed to parse awesome.yml: found character that cannot start any token`, fmt.Sprintf("%s", err))
}

func TestPipelineParserReturnsJsonParsingErrors(t *testing.T) {
	_, err := PipelineParser{Filename: "awesome.json", Pipeline: []byte("{")}.Parse()
	assert.Error(t, err, `Failed to parse awesome.json: line 1: did not find expected node content`, fmt.Sprintf("%s", err))
}

func TestPipelineParserParsesJson(t *testing.T) {
	environ := env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`})

	result, err := PipelineParser{
		Filename: "thing.json",
		Pipeline: []byte("\n\n     \n  { \"foo\": \"bye ${ENV_VAR_FRIEND}\" }\n"),
		Env:      environ}.Parse()

	assert.NoError(t, err)
	j, err := json.Marshal(result)
	assert.Equal(t, `{"foo":"bye \"friend\""}`, string(j))
}

func TestPipelineParserParsesJsonObjects(t *testing.T) {
	environ := env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`})

	result, err := PipelineParser{Pipeline: []byte("\n\n     \n  { \"foo\": \"bye ${ENV_VAR_FRIEND}\" }\n"), Env: environ}.Parse()
	assert.NoError(t, err)
	j, err := json.Marshal(result)
	assert.Equal(t, `{"foo":"bye \"friend\""}`, string(j))
}

func TestPipelineParserParsesJsonArrays(t *testing.T) {
	environ := env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`})

	result, err := PipelineParser{Pipeline: []byte("\n\n     \n  [ { \"foo\": \"bye ${ENV_VAR_FRIEND}\" } ]\n"), Env: environ}.Parse()
	assert.NoError(t, err)
	j, err := json.Marshal(result)
	assert.Equal(t, `{"steps":[{"foo":"bye \"friend\""}]}`, string(j))
}

func TestPipelineParserParsesTopLevelSteps(t *testing.T) {
	result, err := PipelineParser{Pipeline: []byte("---\n- name: Build\n  command: echo hello world\n- wait\n"), Env: nil}.Parse()
	assert.NoError(t, err)
	j, err := json.Marshal(result)
	assert.Equal(t, `{"steps":[{"name":"Build","command":"echo hello world"},"wait"]}`, string(j))
}

func TestPipelineParserPreservesBools(t *testing.T) {
	result, err := PipelineParser{Pipeline: []byte("steps:\n  - trigger: hello\n    async: true")}.Parse()
	assert.Nil(t, err)
	j, err := json.Marshal(result)
	assert.Equal(t, `{"steps":[{"trigger":"hello","async":true}]}`, string(j))
}

func TestPipelineParserPreservesInts(t *testing.T) {
	result, err := PipelineParser{Pipeline: []byte("steps:\n  - label: hello\n    parallelism: 10")}.Parse()
	assert.Nil(t, err)
	j, err := json.Marshal(result)
	assert.Equal(t, `{"steps":[{"label":"hello","parallelism":10}]}`, string(j))
}

func TestPipelineParserPreservesNull(t *testing.T) {
	result, err := PipelineParser{Pipeline: []byte("steps:\n  - wait: ~")}.Parse()
	assert.Nil(t, err)
	j, err := json.Marshal(result)
	assert.Equal(t, `{"steps":[{"wait":null}]}`, string(j))
}

func TestPipelineParserPreservesFloats(t *testing.T) {
	result, err := PipelineParser{Pipeline: []byte("steps:\n  - trigger: hello\n    llamas: 3.142")}.Parse()
	assert.Nil(t, err)
	j, err := json.Marshal(result)
	assert.Equal(t, `{"steps":[{"trigger":"hello","llamas":3.142}]}`, string(j))
}

func TestPipelineParserHandlesDates(t *testing.T) {
	result, err := PipelineParser{Pipeline: []byte("steps:\n  - trigger: hello\n    llamas: 2002-08-15T17:18:23.18-06:00")}.Parse()
	assert.Nil(t, err)
	j, err := json.Marshal(result)
	assert.Equal(t, `{"steps":[{"trigger":"hello","llamas":"2002-08-15T17:18:23.18-06:00"}]}`, string(j))
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

func TestPipelineParserPreservesOrderOfPlugins(t *testing.T) {
	var pipeline = `---
steps:
  - name: ":s3: xxx"
    command: "script/buildkite/xxx.sh"
    plugins:
      xxx/aws-assume-role#v0.1.0:
        role: arn:aws:iam::xxx:role/xxx
      ecr#v1.1.4:
        login: true
        account_ids: xxx
        registry_region: us-east-1
      docker-compose#v2.5.1:
        run: xxx
        config: .buildkite/docker/docker-compose.yml
        env:
          - AWS_ACCESS_KEY_ID
          - AWS_SECRET_ACCESS_KEY
          - AWS_SESSION_TOKEN
    agents:
      queue: xxx`

	result, err := PipelineParser{Pipeline: []byte(pipeline), Env: nil}.Parse()
	if err != nil {
		t.Fatal(err)
	}

	buf := &bytes.Buffer{}
	err = json.NewEncoder(buf).Encode(result)
	if err != nil {
		t.Fatal(err)
	}

	expected := `{"steps":[{"name":":s3: xxx","command":"script/buildkite/xxx.sh","plugins":{"xxx/aws-assume-role#v0.1.0":{"role":"arn:aws:iam::xxx:role/xxx"},"ecr#v1.1.4":{"login":true,"account_ids":"xxx","registry_region":"us-east-1"},"docker-compose#v2.5.1":{"run":"xxx","config":".buildkite/docker/docker-compose.yml","env":["AWS_ACCESS_KEY_ID","AWS_SECRET_ACCESS_KEY","AWS_SESSION_TOKEN"]}},"agents":{"queue":"xxx"}}]}`
	assert.Equal(t, expected, strings.TrimSpace(buf.String()))
}
