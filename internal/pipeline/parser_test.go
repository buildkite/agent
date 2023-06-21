package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/env"
	"github.com/stretchr/testify/assert"
)

func TestParserParsesYaml(t *testing.T) {
	parser := Parser{
		Env:            env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`}),
		Filename:       "awesome.yml",
		PipelineSource: []byte("steps:\n  - command: \"hello ${ENV_VAR_FRIEND}\""),
	}
	result, err := parser.Parse()

	assert.NoError(t, err)
	j, err := json.Marshal(result)
	if err != nil {
		t.Errorf("json.Marshal(result) error = %v", err)
	}
	assert.Equal(t, `{"steps":[{"command":"hello \"friend\""}]}`, string(j))
}

func TestParserParsesYamlWithNoInterpolation(t *testing.T) {
	parser := Parser{
		Filename:        "awesome.yml",
		PipelineSource:  []byte("steps:\n  - command: \"hello ${ENV_VAR_FRIEND}\""),
		NoInterpolation: true,
	}
	result, err := parser.Parse()

	assert.NoError(t, err)
	j, err := json.Marshal(result)
	if err != nil {
		t.Errorf("json.Marshal(result) error = %v", err)
	}
	assert.Equal(t, `{"steps":[{"command":"hello ${ENV_VAR_FRIEND}"}]}`, string(j))
}

func TestParserSupportsYamlMergesAndAnchors(t *testing.T) {
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

	parser := Parser{
		Filename:       "awesome.yml",
		PipelineSource: []byte(complexYAML),
	}
	result, err := parser.Parse()

	assert.NoError(t, err)
	j, err := json.Marshal(result)
	if err != nil {
		t.Errorf("json.Marshal(result) error = %v", err)
	}
	assert.Equal(t, `{"base_step":{"type":"script","agent_query_rules":["queue=default"]},"steps":[{"type":"script","agent_query_rules":["queue=default"],"name":":docker: building image","command":"docker build .","agents":{"queue":"default"}}]}`, string(j))
}

func TestParserReturnsYamlParsingErrors(t *testing.T) {
	parser := Parser{
		Filename:       "awesome.yml",
		PipelineSource: []byte("steps: %blah%"),
	}
	_, err := parser.Parse()

	assert.Error(t, err, `Failed to parse awesome.yml: found character that cannot start any token`, fmt.Sprintf("%s", err))
}

func TestParserReturnsJsonParsingErrors(t *testing.T) {
	parser := Parser{
		Filename:       "awesome.json",
		PipelineSource: []byte("{"),
	}
	_, err := parser.Parse()

	assert.Error(t, err, `Failed to parse awesome.json: line 1: did not find expected node content`, fmt.Sprintf("%s", err))
}

func TestParserParsesJson(t *testing.T) {
	parser := Parser{
		Env:            env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`}),
		Filename:       "thing.json",
		PipelineSource: []byte("\n\n     \n  { \"foo\": \"bye ${ENV_VAR_FRIEND}\" }\n"),
	}
	result, err := parser.Parse()

	assert.NoError(t, err)
	j, err := json.Marshal(result)
	if err != nil {
		t.Errorf("json.Marshal(result) error = %v", err)
	}
	assert.Equal(t, `{"foo":"bye \"friend\""}`, string(j))
}

func TestParserParsesJsonObjects(t *testing.T) {
	parser := Parser{
		Env:            env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`}),
		PipelineSource: []byte("\n\n     \n  { \"foo\": \"bye ${ENV_VAR_FRIEND}\" }\n"),
	}
	result, err := parser.Parse()

	assert.NoError(t, err)
	j, err := json.Marshal(result)
	if err != nil {
		t.Errorf("json.Marshal(result) error = %v", err)
	}
	assert.Equal(t, `{"foo":"bye \"friend\""}`, string(j))
}

func TestParserParsesJsonArrays(t *testing.T) {
	parser := Parser{
		Env:            env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`}),
		PipelineSource: []byte("\n\n     \n  [ { \"foo\": \"bye ${ENV_VAR_FRIEND}\" } ]\n"),
	}
	result, err := parser.Parse()

	assert.NoError(t, err)
	j, err := json.Marshal(result)
	if err != nil {
		t.Errorf("json.Marshal(result) error = %v", err)
	}
	assert.Equal(t, `{"steps":[{"foo":"bye \"friend\""}]}`, string(j))
}

func TestParserParsesTopLevelSteps(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte("---\n- name: Build\n  command: echo hello world\n- wait\n"),
	}
	result, err := parser.Parse()

	assert.NoError(t, err)
	j, err := json.Marshal(result)
	if err != nil {
		t.Errorf("json.Marshal(result) error = %v", err)
	}
	assert.Equal(t, `{"steps":[{"name":"Build","command":"echo hello world"},"wait"]}`, string(j))
}

func TestParserPreservesBools(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte("steps:\n  - trigger: hello\n    async: true"),
	}
	result, err := parser.Parse()

	assert.Nil(t, err)
	j, err := json.Marshal(result)
	if err != nil {
		t.Errorf("json.Marshal(result) error = %v", err)
	}
	assert.Equal(t, `{"steps":[{"trigger":"hello","async":true}]}`, string(j))
}

func TestParserPreservesInts(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte("steps:\n  - label: hello\n    parallelism: 10"),
	}
	result, err := parser.Parse()

	assert.Nil(t, err)
	j, err := json.Marshal(result)
	if err != nil {
		t.Errorf("json.Marshal(result) error = %v", err)
	}
	assert.Equal(t, `{"steps":[{"label":"hello","parallelism":10}]}`, string(j))
}

func TestParserPreservesNull(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte("steps:\n  - wait: ~"),
	}
	result, err := parser.Parse()

	assert.Nil(t, err)
	j, err := json.Marshal(result)
	if err != nil {
		t.Errorf("json.Marshal(result) error = %v", err)
	}
	assert.Equal(t, `{"steps":[{"wait":null}]}`, string(j))
}

func TestParserPreservesFloats(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte("steps:\n  - trigger: hello\n    llamas: 3.142"),
	}
	result, err := parser.Parse()

	assert.Nil(t, err)
	j, err := json.Marshal(result)
	if err != nil {
		t.Errorf("json.Marshal(result) error = %v", err)
	}
	assert.Equal(t, `{"steps":[{"trigger":"hello","llamas":3.142}]}`, string(j))
}

func TestParserHandlesDates(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte("steps:\n  - trigger: hello\n    llamas: 2002-08-15T17:18:23.18-06:00"),
	}
	result, err := parser.Parse()

	assert.Nil(t, err)
	j, err := json.Marshal(result)
	if err != nil {
		t.Errorf("json.Marshal(result) error = %v", err)
	}
	assert.Equal(t, `{"steps":[{"trigger":"hello","llamas":"2002-08-15T17:18:23.18-06:00"}]}`, string(j))
}

func TestParserInterpolatesKeysAsWellAsValues(t *testing.T) {
	var pipeline = `{
		"env": {
			"${FROM_ENV}TEST1": "MyTest",
			"TEST2": "${FROM_ENV}"
		}
	}`

	var decoded struct {
		Env map[string]string `json:"env"`
	}

	parser := Parser{
		Env:            env.FromSlice([]string{"FROM_ENV=llamas"}),
		PipelineSource: []byte(pipeline),
	}
	result, err := parser.Parse()

	if err != nil {
		t.Fatalf("Parser{}.Parse() error = %v", err)
	}
	if err := decodeIntoStruct(&decoded, result); err != nil {
		t.Fatalf("decodeIntoStruct(&decoded, result) error = %v", err)
	}
	assert.Equal(t, `MyTest`, decoded.Env["llamasTEST1"])
	assert.Equal(t, `llamas`, decoded.Env["TEST2"])
}

func TestParserLoadsGlobalEnvBlockFirst(t *testing.T) {
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

	parser := Parser{
		PipelineSource: []byte(pipeline),
		Env:            env.FromSlice([]string{"YEAR_FROM_SHELL=1912"}),
	}
	result, err := parser.Parse()

	if err != nil {
		t.Fatalf("Parser{}.Parse() error = %v", err)
	}
	if err := decodeIntoStruct(&decoded, result); err != nil {
		t.Fatalf("decodeIntoStruct(&decoded, result) error = %v", err)
	}
	assert.Equal(t, "England", decoded.Env["TEAM1"])
	assert.Equal(t, "England smashes Australia to win the ashes in 1912!!", decoded.Env["HEADLINE"])
	assert.Equal(t, "echo England smashes Australia to win the ashes in 1912!!", decoded.Steps[0].Command)
}

func decodeIntoStruct(into any, from any) error {
	b, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, into)
}

func TestParserPreservesOrderOfPlugins(t *testing.T) {
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

	parser := Parser{PipelineSource: []byte(pipeline), Env: nil}
	result, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parser{}.Parse() error = %v", err)
	}

	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(result); err != nil {
		t.Fatalf("json.NewEncoder(buf).Encode(result) = %v", err)
	}

	expected := `{"steps":[{"name":":s3: xxx","command":"script/buildkite/xxx.sh","plugins":{"xxx/aws-assume-role#v0.1.0":{"role":"arn:aws:iam::xxx:role/xxx"},"ecr#v1.1.4":{"login":true,"account_ids":"xxx","registry_region":"us-east-1"},"docker-compose#v2.5.1":{"run":"xxx","config":".buildkite/docker/docker-compose.yml","env":["AWS_ACCESS_KEY_ID","AWS_SECRET_ACCESS_KEY","AWS_SESSION_TOKEN"]}},"agents":{"queue":"xxx"}}]}`
	assert.Equal(t, expected, strings.TrimSpace(buf.String()))
}

func TestParserParsesConditionalWithEndOfLineAnchorDollarSign(t *testing.T) {
	for _, row := range []struct {
		noInterpolation bool
		pipeline        string
	}{
		// dollar sign must be escaped when interpolation is in effect
		{false, "steps:\n  - if: build.env(\"ACCOUNT\") =~ /^(foo|bar)\\$/"},
		{true, "steps:\n  - if: build.env(\"ACCOUNT\") =~ /^(foo|bar)$/"},
	} {
		parser := Parser{
			PipelineSource:  []byte(row.pipeline),
			NoInterpolation: row.noInterpolation,
		}
		result, err := parser.Parse()
		assert.NoError(t, err)
		j, _ := json.Marshal(result)
		assert.Equal(t, `{"steps":[{"if":"build.env(\"ACCOUNT\") =~ /^(foo|bar)$/"}]}`, string(j))
	}
}

func TestPipelinePropagatesTracingDataIfAvailable(t *testing.T) {
	e := env.New()
	e.Set("BUILDKITE_TRACE_CONTEXT", "123")
	for _, row := range []struct {
		hasExistingEnv bool
		expected       string
	}{
		{false, `{"steps":[{"command":"echo asd"}],"env":{"BUILDKITE_TRACE_CONTEXT":"123"}}`},
		{true, `{"steps":[{"command":"echo asd"}],"env":{"ASD":1,"BUILDKITE_TRACE_CONTEXT":"123"}}`},
	} {
		pipelineYaml := "steps:\n  - command: echo asd\n"
		if row.hasExistingEnv {
			pipelineYaml += "env:\n  ASD: 1"
		}
		parser := Parser{
			PipelineSource: []byte(pipelineYaml),
			Env:            e,
		}
		result, err := parser.Parse()
		assert.NoError(t, err)
		j, _ := json.Marshal(result)
		assert.Equal(t, row.expected, string(j))
	}
}
