package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/buildkite/agent/shell"
)

type Plugin struct {
	// Where the plugin can be found (can either be a file system path, or
	// a git repository)
	Location string

	// The version of the plugin that should be running
	Version string

	// Configuration for the plugin
	Configuration map[string]interface{}
}

func CreatePlugin(location string, config map[string]interface{}) (*Plugin, error) {
	plugin := &Plugin{Configuration: config}

	// Extract the version from the location
	parts := strings.Split(location, "#")
	if len(parts) > 2 {
		return nil, fmt.Errorf("Too many #'s in \"%s\"", location)
	} else if len(parts) == 2 {
		plugin.Location = parts[0]
		plugin.Version = parts[1]
	} else {
		plugin.Location = location
	}

	return plugin, nil
}

// Given a JSON structure, convert it to an array of plugins
func CreatePluginsFromJSON(j string) ([]*Plugin, error) {
	// Parse the JSON
	var f interface{}
	err := json.Unmarshal([]byte(j), &f)
	if err != nil {
		return nil, err
	}

	// Try and convert the structure to an array
	m, ok := f.([]interface{})
	if !ok {
		return nil, fmt.Errorf("JSON structure was not an array")
	}

	// Convert the JSON elements to plugins
	plugins := []*Plugin{}
	for _, v := range m {
		switch vv := v.(type) {
		case string:
			// Add the plugin with no config to the array
			plugin, err := CreatePlugin(string(vv), map[string]interface{}{})
			if err != nil {
				return nil, err
			}
			plugins = append(plugins, plugin)
		case map[string]interface{}:
			for location, config := range vv {
				// Ensure the config is a hash
				config, ok := config.(map[string]interface{})
				if !ok {
					return nil, fmt.Errorf("Configuration for \"%s\" is not a hash", location)
				}

				// Add the plugin with config to the array
				plugin, err := CreatePlugin(string(location), config)
				if err != nil {
					return nil, err
				}
				plugins = append(plugins, plugin)
			}
		default:
			return nil, fmt.Errorf("Unknown type in plugin definition (%s)", vv)
		}
	}

	return plugins, nil
}

// Returns the name of the plugin
func (p *Plugin) Name() string {
	if p.Location != "" {
		// Grab the last part of the location
		parts := strings.Split(p.Location, "/")
		name := parts[len(parts)-1]

		// Clean up the name
		name = strings.ToLower(name)
		name = regexp.MustCompile(`\s+`).ReplaceAllString(name, " ")
		name = regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(name, "-")

		return name
	} else {
		return ""
	}
}

// Returns and ID for the plugin that can be used as a folder name
func (p *Plugin) Identifier() (string, error) {
	nonIdCharacterRegex := regexp.MustCompile(`[^a-zA-Z0-9]`)
	removeDoubleUnderscore := regexp.MustCompile(`-+`)

	id := p.Label()
	id = nonIdCharacterRegex.ReplaceAllString(id, "-")
	id = removeDoubleUnderscore.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")

	return id, nil
}

// Returns the repository host where the code is stored
func (p *Plugin) Repository() (string, error) {
	if p.Location == "" {
		return "", fmt.Errorf("Missing plugin location")
	}

	parts := strings.Split(p.Location, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("Incomplete plugin path \"%s\"", p.Location)
	}

	if parts[0] == "github.com" || parts[0] == "bitbucket.org" || parts[0] == "gitlab.com" {
		if len(parts) < 3 {
			return "", fmt.Errorf("Incomplete %s path \"%s\"", parts[0], p.Location)
		}

		return strings.Join(parts[:3], "/"), nil
	} else {
		repo := []string{}

		for _, p := range parts {
			repo = append(repo, p)

			if strings.HasSuffix(p, ".git") {
				break
			}
		}

		return strings.Join(repo, "/"), nil
	}

	return "", nil
}

// Returns the subdirectory path that the plugin is in
func (p *Plugin) RepositorySubdirectory() (string, error) {
	repository, err := p.Repository()
	if err != nil {
		return "", err
	}

	dir := strings.TrimPrefix(p.Location, repository)

	return strings.TrimPrefix(dir, "/"), nil
}

// Converts the plugin configuration values to environment variables
func (p *Plugin) ConfigurationToEnvironment() (*shell.Environment, error) {
	env := []string{}

	toDashRegex := regexp.MustCompile(`-|\s+`)
	removeWhitespaceRegex := regexp.MustCompile(`\s+`)
	removeDoubleUnderscore := regexp.MustCompile(`_+`)

	for k, v := range p.Configuration {
		k = removeWhitespaceRegex.ReplaceAllString(k, " ")
		name := strings.ToUpper(toDashRegex.ReplaceAllString(fmt.Sprintf("BUILDKITE_PLUGIN_%s_%s", p.Name(), k), "_"))
		name = removeDoubleUnderscore.ReplaceAllString(name, "_")

		switch vv := v.(type) {
		case string:
			env = append(env, fmt.Sprintf("%s=%s", name, vv))
		case int:
			env = append(env, fmt.Sprintf("%s=%d", name, vv))
		default:
			// unknown type
		}
	}

	// Sort them into a consistent order
	sort.Strings(env)

	return shell.EnvironmentFromSlice(env)
}

// Pretty name for the plugin
func (p *Plugin) Label() string {
	if p.Version != "" {
		return p.Location + "#" + p.Version
	} else {
		return p.Location
	}
}
