package plugin

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/buildkite/yaml"
)

var ErrDefinitionNotFound = errors.New("Definition file not found")

// Definition defines the plugin.yml file that each plugin has
type Definition struct {
	Name         string   `yaml:"name"`
	Requirements []string `yaml:"requirements"`
}

// ParseDefinition parses either yaml or json bytes into a Definition
func ParseDefinition(b []byte) (*Definition, error) {
	var def Definition

	if err := yaml.Unmarshal(b, &def); err != nil {
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
