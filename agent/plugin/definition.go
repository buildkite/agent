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

	"github.com/buildkite/agent/yamltojson"
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
	var parsed interface{}

	// Parse the definition as a json compatible string map, the plain yaml
	// Unmarshal returns a structure that has map[interface{}]interface{}
	// which causes anything that expects map[string]interface{} to break
	if err := yamltojson.UnmarshalAsStringMap(b, &parsed); err != nil {
		return nil, err
	}

	// Check we've got a map, vs a list or something else, this lets us
	// give a useful error message
	parsedMap, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Failed to convert %T to map[string]interface{}", parsed)
	}

	// Marshal the whole lot back into json which will let the jsonschema library
	// parse the schema into and object tree 💃🏼
	jsonBytes, err := json.Marshal(parsedMap)
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
		if _, err := os.Stat(filename); os.IsExist(err) {
			return filename, nil
		}
	}
	return "", ErrDefinitionNotFound
}

type Validator struct {
	CommandExists func(string) bool
}

func (v Validator) Validate(def *Definition, config map[string]interface{}) ValidateResult {
	result := ValidateResult{
		Valid:  true,
		Errors: []string{},
	}

	configAsJson, err := json.Marshal(config)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result
	}

	var commandExistsFunc = v.CommandExists
	if commandExistsFunc == nil {
		commandExistsFunc = commandExists
	}

	// validate that the required commands exist
	for _, command := range def.Requirements {
		if !commandExistsFunc(command) {
			result.Valid = false
			result.Errors = append(result.Errors,
				fmt.Sprintf(`Required command %q isn't in PATH`, command))
		}
	}

	// validate that the config matches the json schema we have
	if def.Configuration != nil {
		valErrors, err := def.Configuration.ValidateBytes(configAsJson)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		}
		if len(valErrors) > 0 {
			result.Valid = false
			for _, err := range valErrors {
				// fmt.Printf("%#v", err)
				result.Errors = append(result.Errors,
					fmt.Sprintf("Plugin validation failed at %v", err.Error()))
			}
		}
	}

	return result
}

type ValidateResult struct {
	Valid  bool
	Errors []string
}

func (vr ValidateResult) Error() string {
	return "Validation errors: " + strings.Join(vr.Errors, ", ")
}

func commandExists(command string) bool {
	if _, err := exec.LookPath(command); err != nil {
		return false
	}
	return true
}
