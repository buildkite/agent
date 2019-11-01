package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/v3/yamltojson"
	"github.com/buildkite/yaml"
	"github.com/qri-io/jsonschema"
)

var ErrDefinitionNotFound = errors.New("Definition file not found")

// Definition defines the plugin.yml file that each plugin has
type Definition struct {
	Name          string                 `json:"name"`
	Requirements  []string               `json:"requirements"`
	Configuration *jsonschema.RootSchema `json:"configuration"`
}

// ParseDefinition parses either yaml or json bytes into a Definition
func ParseDefinition(b []byte) (*Definition, error) {
	var parsed yaml.MapSlice

	if err := yaml.Unmarshal(b, &parsed); err != nil {
		return nil, err
	}

	// Marshal the whole lot back into json which will let the jsonschema library
	// parse the schema into and object tree 💃🏼
	jsonBytes, err := yamltojson.MarshalMapSliceJSON(parsed)
	if err != nil {
		return nil, err
	}

	var def Definition
	if err := json.Unmarshal(jsonBytes, &def); err != nil {
		return nil, err
	}

	return &def, nil
}

// LoadDefinitionFromDir looks in a directory for either a plugin.json or a plugin.yml
func LoadDefinitionFromDir(dir string) (*Definition, error) {
	f, err := findDefinitionFile(dir)
	if err != nil {
		return nil, err
	}

	b, err := ioutil.ReadFile(f)
	if err != nil {
		return nil, err
	}

	return ParseDefinition(b)
}

// findDefinitionFile searches for known plugin definition files
func findDefinitionFile(dir string) (string, error) {
	var possibleFilenames = []string{
		filepath.Join(dir, `plugin.json`),
		filepath.Join(dir, `plugin.yaml`),
		filepath.Join(dir, `plugin.yml`),
	}
	for _, filename := range possibleFilenames {
		if _, err := os.Stat(filename); err == nil {
			return filename, nil
		}
	}
	return "", ErrDefinitionNotFound
}

type Validator struct {
	commandExists func(string) bool
}

func (v Validator) Validate(def *Definition, config map[string]interface{}) ValidateResult {
	result := ValidateResult{
		Errors: []string{},
	}

	configAsJson, err := json.Marshal(config)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}

	var commandExistsFunc = v.commandExists
	if commandExistsFunc == nil {
		commandExistsFunc = commandExists
	}

	// validate that the required commands exist
	if def.Requirements != nil {
		for _, command := range def.Requirements {
			if !commandExistsFunc(command) {
				result.Errors = append(result.Errors,
					fmt.Sprintf(`Required command %q isn't in PATH`, command))
			}
		}
	}

	// validate that the config matches the json schema we have
	if def.Configuration != nil {
		valErrors, err := def.Configuration.ValidateBytes(configAsJson)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
		}
		if len(valErrors) > 0 {
			for _, err := range valErrors {
				result.Errors = append(result.Errors, err.Error())
			}
		}
	}

	return result
}

type ValidateResult struct {
	Errors []string
}

func (vr ValidateResult) Valid() bool {
	return len(vr.Errors) == 0
}

func (vr ValidateResult) Error() string {
	return strings.Join(vr.Errors, ", ")
}

func commandExists(command string) bool {
	if _, err := exec.LookPath(command); err != nil {
		return false
	}
	return true
}
