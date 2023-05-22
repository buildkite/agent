package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/v3/yamltojson"
	"github.com/qri-io/jsonschema"
	"gopkg.in/yaml.v3"
)

var (
	// ErrDefinitionNotFound is used when a plugin definition file cannot be
	// found.
	ErrDefinitionNotFound = errors.New("Definition file not found")

	// ErrCommandNotInPATH is the underlying error when a command cannot be
	// found during plugin validation.
	ErrCommandNotInPATH = errors.New("command not found in PATH")
)

// Definition defines the contents of the plugin.{yml,yaml,json} file that
// each plugin has.
type Definition struct {
	Name          string             `json:"name"`
	Requirements  []string           `json:"requirements"`
	Configuration *jsonschema.Schema `json:"configuration"`
}

// ParseDefinition parses either YAML or JSON bytes into a Definition.
func ParseDefinition(b []byte) (*Definition, error) {
	var parsed yaml.Node

	if err := yaml.Unmarshal(b, &parsed); err != nil {
		return nil, err
	}

	// Marshal the whole lot back into json which will let the jsonschema library
	// parse the schema into and object tree 💃🏼
	var buf bytes.Buffer
	if err := yamltojson.Encode(&buf, &parsed); err != nil {
		return nil, err
	}

	var def Definition
	if err := json.NewDecoder(&buf).Decode(&def); err != nil {
		return nil, err
	}

	return &def, nil
}

// LoadDefinitionFromDir looks in a directory for one of plugin.json,
// plugin.yaml, or plugin.yml. It parses the first one it finds, and returns the
// resulting Definition. If none of those files can be found, it returns
// ErrDefinitionNotFound.
func LoadDefinitionFromDir(dir string) (*Definition, error) {
	f, err := findDefinitionFile(dir)
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}

	return ParseDefinition(b)
}

// findDefinitionFile searches for known plugin definition files.
func findDefinitionFile(dir string) (string, error) {
	possibleFilenames := []string{
		filepath.Join(dir, "plugin.json"),
		filepath.Join(dir, "plugin.yaml"),
		filepath.Join(dir, "plugin.yml"),
	}
	for _, filename := range possibleFilenames {
		if _, err := os.Stat(filename); err == nil {
			return filename, nil
		}
	}
	return "", ErrDefinitionNotFound
}

// Validator validates plugin definitions.
type Validator struct {
	commandExists func(string) bool
}

// Validate checks the plugin definition for errors, including missing commands
// from $PATH and invalid configuration under the definition's JSON Schema.
func (v Validator) Validate(def *Definition, config map[string]any) ValidateResult {
	var result ValidateResult

	configJSON, err := json.Marshal(config)
	if err != nil {
		result.errors = append(result.errors, err)
		return result
	}

	commandExistsFunc := v.commandExists
	if commandExistsFunc == nil {
		commandExistsFunc = commandExists
	}

	// validate that the required commands exist
	if def.Requirements != nil {
		for _, command := range def.Requirements {
			if commandExistsFunc(command) {
				continue
			}
			result.errors = append(result.errors, fmt.Errorf("%q %w", command, ErrCommandNotInPATH))
		}
	}

	// validate that the config matches the json schema we have
	if def.Configuration != nil {
		valErrors, err := def.Configuration.ValidateBytes(context.TODO(), configJSON)
		if err != nil {
			result.errors = append(result.errors, err)
		}
		for _, err := range valErrors {
			result.errors = append(result.errors, err)
		}
	}

	return result
}

// ValidateResult contains results of a validation check.
type ValidateResult struct {
	errors []error
}

// Unwrap returns the errors contained in the ValidateResult.
func (vr ValidateResult) Unwrap() []error {
	// Support for multi-error wrapping is expected in Go 1.20.
	// https://github.com/golang/go/issues/53435#issuecomment-1191752789
	return vr.errors
}

// Valid reports if the result contains no errors.
func (vr ValidateResult) Valid() bool {
	return len(vr.errors) == 0
}

// Error returns a single string representing all the inner error strings.
func (vr ValidateResult) Error() string {
	s := make([]string, len(vr.errors))
	for i, err := range vr.errors {
		s[i] = err.Error()
	}
	return strings.Join(s, ", ")
}

// commandExists reports if the command is present somewhere in $PATH.
func commandExists(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}
