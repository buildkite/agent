package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPipelineParser(t *testing.T) {
	var err error
	var result interface{}
	var j []byte

	os.Setenv("ENV_VAR_FRIEND", "\"friend\"")

	// It parses pipelines with .yml filenames
	result, err = PipelineParser{Filename: "awesome.yml", Pipeline: []byte("steps:\n  - label: \"hello ${ENV_VAR_FRIEND}\"")}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, string(j), `{"steps":[{"label":"hello \"friend\""}]}`)

	// It parses pipelines with .yaml filenames
	result, err = PipelineParser{Filename: "awesome.yaml", Pipeline: []byte("steps:\n  - label: \"hello ${ENV_VAR_FRIEND}\"")}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, string(j), `{"steps":[{"label":"hello \"friend\""}]}`)

	// Returns YAML parsing errors
	result, err = PipelineParser{Filename: "awesome.yml", Pipeline: []byte("steps: %blah%")}.Parse()
	assert.NotNil(t, err)
	assert.Equal(t, fmt.Sprintf("%s", err), `Failed to parse YAML: [while scanning for the next token] found character that cannot start any token at line 1, column 8`)

	// Returns JSON parsing errors
	result, err = PipelineParser{Filename: "awesome.json", Pipeline: []byte("{")}.Parse()
	assert.NotNil(t, err)
	assert.Equal(t, fmt.Sprintf("%s", err), `Failed to parse JSON: unexpected end of JSON input`)

	// It parses pipelines with .json filenames
	result, err = PipelineParser{Filename: "thing.json", Pipeline: []byte("\n\n     \n  { \"foo\": \"bye ${ENV_VAR_FRIEND}\" }\n")}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, string(j), `{"foo":"bye \"friend\""}`)

	// It parses unknown YAML
	result, err = PipelineParser{Pipeline: []byte("steps:\n  - label: \"hello ${ENV_VAR_FRIEND}\"")}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, string(j), `{"steps":[{"label":"hello \"friend\""}]}`)

	// Returns YAML parsing errors if the content looks like YAML
	result, err = PipelineParser{Pipeline: []byte("steps: %blah%")}.Parse()
	assert.NotNil(t, err)
	assert.Equal(t, fmt.Sprintf("%s", err), `Failed to parse YAML: [while scanning for the next token] found character that cannot start any token at line 1, column 8`)

	// It parses unknown JSON objects
	result, err = PipelineParser{Pipeline: []byte("\n\n     \n  { \"foo\": \"bye ${ENV_VAR_FRIEND}\" }\n")}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, string(j), `{"foo":"bye \"friend\""}`)

	// It parses unknown JSON arrays
	result, err = PipelineParser{Pipeline: []byte("\n\n     \n  [ { \"foo\": \"bye ${ENV_VAR_FRIEND}\" } ]\n")}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, string(j), `[{"foo":"bye \"friend\""}]`)

	// Returns JSON parsing errors if the content looks like JSON
	result, err = PipelineParser{Pipeline: []byte("{")}.Parse()
	assert.NotNil(t, err)
	assert.Equal(t, fmt.Sprintf("%s", err), `Failed to parse JSON: unexpected end of JSON input`)
}
