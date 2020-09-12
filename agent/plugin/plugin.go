package plugin

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/buildkite/agent/v3/env"
)

type Plugin struct {
	// Where the plugin can be found (can either be a file system path, or
	// a git repository)
	Location string

	// The version of the plugin that should be running
	Version string

	// The clone method
	Scheme string

	// Any authentication attached to the repository
	Authentication string

	// Whether the plugin refers to a vendored path
	Vendored bool

	// Configuration for the plugin
	Configuration map[string]interface{}
}

var (
	locationSchemeRegex = regexp.MustCompile(`^[a-z\+]+://`)
	vendoredRegex       = regexp.MustCompile(`^\.`)
)

func CreatePlugin(location string, config map[string]interface{}) (*Plugin, error) {
	plugin := &Plugin{Configuration: config}

	u, err := url.Parse(location)
	if err != nil {
		return nil, err
	}

	plugin.Scheme = u.Scheme
	plugin.Location = u.Host + u.Path
	plugin.Version = u.Fragment
	plugin.Vendored = vendoredRegex.MatchString(plugin.Location)

	if plugin.Version != "" && strings.Count(plugin.Version, "#") > 0 {
		return nil, fmt.Errorf("Too many #'s in \"%s\"", location)
	}

	if u.User != nil {
		plugin.Authentication = u.User.String()
	}

	return plugin, nil
}

// Given a JSON structure, convert it to an array of plugins
func CreateFromJSON(j string) ([]*Plugin, error) {
	// Use more versatile number decoding
	decoder := json.NewDecoder(strings.NewReader(j))
	decoder.UseNumber()

	// Parse the JSON
	var f interface{}
	err := decoder.Decode(&f)
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
				// Plugins without configs are easy!
				if config == nil {
					plugin, err := CreatePlugin(string(location), map[string]interface{}{})
					if err != nil {
						return nil, err
					}

					plugins = append(plugins, plugin)
					continue
				}

				// Since there is a config, it's gotta be a hash
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
		// for filepaths, we can get windows backslashes, so we normalize them
		location := strings.Replace(p.Location, "\\", "/", -1)

		// Grab the last part of the location
		parts := strings.Split(location, "/")
		name := parts[len(parts)-1]

		// Clean up the name
		name = strings.ToLower(name)
		name = regexp.MustCompile(`\s+`).ReplaceAllString(name, " ")
		name = regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(name, "-")
		name = strings.Replace(name, "-buildkite-plugin-git", "", -1)
		name = strings.Replace(name, "-buildkite-plugin", "", -1)

		return name
	} else {
		return ""
	}
}

// Returns an ID for the plugin that can be used as a folder name
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
	s, err := p.constructRepositoryHost()
	if err != nil {
		return "", err
	}

	// Add the authentication if there is one
	if p.Authentication != "" {
		s = p.Authentication + "@" + s
	}

	// If it's not a file system plugin, add the scheme
	if !strings.HasPrefix(s, "/") {
		if p.Scheme != "" {
			s = p.Scheme + "://" + s
		} else {
			s = "https://" + s
		}
	}

	return s, nil
}

// Returns the subdirectory path that the plugin is in
func (p *Plugin) RepositorySubdirectory() (string, error) {
	repository, err := p.constructRepositoryHost()
	if err != nil {
		return "", err
	}

	dir := strings.TrimPrefix(p.Location, repository)

	return strings.TrimPrefix(dir, "/"), nil
}

var (
	toDashRegex            = regexp.MustCompile(`-|\s+`)
	removeWhitespaceRegex  = regexp.MustCompile(`\s+`)
	removeDoubleUnderscore = regexp.MustCompile(`_+`)
)

// formatEnvKey converts strings into an ENV key friendly format
func formatEnvKey(key string) string {
	key = strings.ToUpper(key)
	key = removeWhitespaceRegex.ReplaceAllString(key, " ")
	key = toDashRegex.ReplaceAllString(key, "_")
	key = removeDoubleUnderscore.ReplaceAllString(key, "_")
	return key
}

func walkConfigValues(prefix string, v interface{}, into *[]string) error {
	switch vv := v.(type) {

	// handles all of our primitive types, golang provides a good string representation
	case string, bool, json.Number:
		*into = append(*into, fmt.Sprintf("%s=%v", prefix, vv))
		return nil

	// handle lists of things, which get a KEY_N prefix depending on the index
	case []interface{}:
		for i := range vv {
			if err := walkConfigValues(fmt.Sprintf("%s_%d", prefix, i), vv[i], into); err != nil {
				return err
			}
		}
		return nil

	// handle maps of things, which get a KEY_SUBKEY prefix depending on the map keys
	case map[string]interface{}:
		for k, vvv := range vv {
			if err := walkConfigValues(fmt.Sprintf("%s_%s", prefix, formatEnvKey(k)), vvv, into); err != nil {
				return err
			}
		}
		return nil
	}

	return fmt.Errorf("Unknown type %T %v", v, v)
}

// Converts the plugin configuration values to environment variables
func (p *Plugin) ConfigurationToEnvironment() (*env.Environment, error) {
	envSlice := []string{}
	envPrefix := fmt.Sprintf("BUILDKITE_PLUGIN_%s", formatEnvKey(p.Name()))

	for k, v := range p.Configuration {
		configPrefix := fmt.Sprintf("%s_%s", envPrefix, formatEnvKey(k))
		if err := walkConfigValues(configPrefix, v, &envSlice); err != nil {
			return nil, err
		}
	}

	// Sort them into a consistent order
	sort.Strings(envSlice)

	return env.FromSlice(envSlice), nil
}

// Pretty name for the plugin
func (p *Plugin) Label() string {
	if p.Version != "" {
		return p.Location + "#" + p.Version
	} else {
		return p.Location
	}
}

func (p *Plugin) constructRepositoryHost() (string, error) {
	if p.Location == "" {
		return "", fmt.Errorf("Missing plugin location")
	}

	parts := strings.Split(p.Location, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("Incomplete plugin path \"%s\"", p.Location)
	}

	var s string

	if parts[0] == "github.com" || parts[0] == "bitbucket.org" || parts[0] == "gitlab.com" {
		if len(parts) < 3 {
			return "", fmt.Errorf("Incomplete %s path \"%s\"", parts[0], p.Location)
		}

		s = strings.Join(parts[:3], "/")
	} else {
		repo := []string{}

		for _, p := range parts {
			repo = append(repo, p)

			if strings.HasSuffix(p, ".git") {
				break
			}
		}

		s = strings.Join(repo, "/")
	}

	return s, nil
}
