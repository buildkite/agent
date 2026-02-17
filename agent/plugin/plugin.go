// Package plugin provides types for managing agent plugins.
//
// It is intended for internal use by buildkite-agent only.
package plugin

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/buildkite/agent/v3/env"
)

var (
	nonIDCharacterRE        = regexp.MustCompile(`[^a-zA-Z0-9]`)
	consecutiveHyphenRE     = regexp.MustCompile(`-+`)
	hypenOrSpaceRE          = regexp.MustCompile(`-|\s`)
	whitespaceRE            = regexp.MustCompile(`\s+`)
	consecutiveUnderscoreRE = regexp.MustCompile(`_+`)
)

// Plugin describes where to find, and how to configure, an agent plugin.
type Plugin struct {
	// Where the plugin can be found (can either be a file system path, or
	// a git repository).
	Location string

	// The version of the plugin that should be running.
	Version string

	// The clone method.
	Scheme string

	// Any authentication attached to the repository.
	Authentication string

	// Whether the plugin refers to a vendored path.
	Vendored bool

	// Configuration for the plugin.
	Configuration map[string]any
}

// CreatePlugin returns a Plugin for the given location and config.
func CreatePlugin(location string, config map[string]any) (*Plugin, error) {
	plugin := &Plugin{Configuration: config}

	u, err := url.Parse(location)
	if err != nil {
		return nil, err
	}

	plugin.Scheme = u.Scheme
	plugin.Location = u.Host + u.Path
	plugin.Version = u.Fragment
	plugin.Vendored = strings.HasPrefix(plugin.Location, ".")

	if plugin.Version != "" && strings.Count(plugin.Version, "#") > 0 {
		return nil, fmt.Errorf("Too many '#'s in %q", location)
	}

	if u.User != nil {
		plugin.Authentication = u.User.String()
	}

	return plugin, nil
}

// CreateFromJSON returns a slice of Plugins loaded from a JSON string.
func CreateFromJSON(j string) ([]*Plugin, error) {
	// Use more versatile number decoding
	decoder := json.NewDecoder(strings.NewReader(j))
	decoder.UseNumber()

	// Parse the JSON
	var f any
	if err := decoder.Decode(&f); err != nil {
		return nil, err
	}

	// Try and convert the structure to an array
	m, ok := f.([]any)
	if !ok {
		return nil, fmt.Errorf("JSON structure was not an array")
	}

	// Convert the JSON elements to plugins
	plugins := []*Plugin{}
	for _, v := range m {
		switch vv := v.(type) {
		case string:
			// Add the plugin with no config to the array
			plugin, err := CreatePlugin(vv, map[string]any{})
			if err != nil {
				return nil, err
			}
			plugins = append(plugins, plugin)

		case map[string]any:
			for location, config := range vv {
				// Plugins without configs are easy!
				if config == nil {
					plugin, err := CreatePlugin(location, map[string]any{})
					if err != nil {
						return nil, err
					}

					plugins = append(plugins, plugin)
					continue
				}

				// Since there is a config, it's gotta be a hash
				config, ok := config.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("Configuration for \"%s\" is not a hash", location)
				}

				// Add the plugin with config to the array
				plugin, err := CreatePlugin(location, config)
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

// IsZipPlugin returns true if this plugin is a zip archive.
func (p *Plugin) IsZipPlugin() bool {
	return strings.HasPrefix(p.Scheme, "zip+")
}

// ZipBaseScheme returns the base scheme for a zip plugin (e.g., "https" for "zip+https"),
// or the scheme itself if it's not a zip plugin.
func (p *Plugin) ZipBaseScheme() string {
	if p.IsZipPlugin() {
		return strings.TrimPrefix(p.Scheme, "zip+")
	}
	return p.Scheme
}

// Name returns the name of the plugin.
func (p *Plugin) Name() string {
	if p.Location == "" {
		return ""
	}
	// for filepaths, we can get windows backslashes, so we normalize them
	location := strings.ReplaceAll(p.Location, "\\", "/")
	location = strings.TrimRight(location, "/") // Trailing slash is useless and will confuse the subsequent parsing

	// Grab the last part of the location
	parts := strings.Split(location, "/")
	name := parts[len(parts)-1]

	// If the last path segment starts with a dot (e.g. ".buildkite"), trim leading dots
	// so the human-friendly name isn't rendered as "-buildkite" after normalization.
	if strings.HasPrefix(name, ".") {
		name = strings.TrimLeft(name, ".")
	}

	// Clean up the name
	name = strings.ToLower(name)
	name = whitespaceRE.ReplaceAllString(name, " ")
	name = nonIDCharacterRE.ReplaceAllString(name, "-")
	name = strings.ReplaceAll(name, "-buildkite-plugin-git", "")
	name = strings.ReplaceAll(name, "-buildkite-plugin", "")

	return name
}

// Identifier returns an ID for the plugin that can be used as a folder name.
func (p *Plugin) Identifier() (string, error) {
	id := p.Label()
	id = nonIDCharacterRE.ReplaceAllString(id, "-")
	id = consecutiveHyphenRE.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")

	return id, nil
}

// Repository returns the repository URL where the plugin code is stored, without any subdirectory path.
// For example, for "github.com/buildkite/plugins/docker-compose/plugin", it returns "github.com/buildkite/plugins".
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

// RepositorySubdirectory returns the subdirectory path that the plugin is in.
// For example, for "github.com/buildkite/plugins/docker-compose/plugin", it returns "docker-compose/plugin".
// If the plugin is in the root of the repository, it returns an empty string.
func (p *Plugin) RepositorySubdirectory() (string, error) {
	repository, err := p.constructRepositoryHost()
	if err != nil {
		return "", err
	}

	dir := strings.TrimPrefix(p.Location, repository)
	dir = strings.TrimPrefix(dir, "/")

	// Remove .git suffix if present as it's not part of the subdirectory
	dir = strings.TrimSuffix(dir, ".git")

	return dir, nil
}

// formatEnvKey converts strings into an ENV key friendly format
func formatEnvKey(key string) string {
	newKey := strings.ToUpper(key)
	return hypenOrSpaceRE.ReplaceAllString(newKey, "_")
}

func walkConfigValues(prefix string, v any, into *[]string) error {
	switch vv := v.(type) {
	// handles all of our primitive types, golang provides a good string representation
	case string, bool, json.Number:
		*into = append(*into, fmt.Sprintf("%s=%v", prefix, vv))
		return nil

	// handle lists of things, which get a KEY_N prefix depending on the index
	case []any:
		for i := range vv {
			if err := walkConfigValues(fmt.Sprintf("%s_%d", prefix, i), vv[i], into); err != nil {
				return err
			}
		}
		return nil

	// handle maps of things, which get a KEY_SUBKEY prefix depending on the map keys
	case map[string]any:
		for k, vvv := range vv {
			if err := walkConfigValues(fmt.Sprintf("%s_%s", prefix, formatEnvKey(k)), vvv, into); err != nil {
				return err
			}
		}
		return nil
	}

	return fmt.Errorf("Unknown type %T %v", v, v)
}

// The input should be a slice of Env Variables of the form `k=v` where `k` is the variable name
// and `v` is its value. If it can be determined that any of the env variables names is part of a
// deprecation, either as the deprecated variable name or its replacement, append both to the
// returned slice, and also append a deprecation error to the error value. If there are no
// deprecations, the error value is guaranteed to be nil.
func fanOutDeprecatedEnvVarNames(envSlice []string) ([]string, error) {
	envSliceAfter := envSlice

	var dnerrs *DeprecatedNameErrors
	for _, kv := range envSlice {
		k, v, ok := strings.Cut(kv, "=")
		if !ok { // this is impossible if the precondition is met
			continue
		}

		// the form with consecutive underscores is replacing the form without, but the replacement
		// is what is expected to be in input slice
		noConsecutiveUnderScoreKey := consecutiveUnderscoreRE.ReplaceAllString(k, "_")
		if k != noConsecutiveUnderScoreKey {
			envSliceAfter = append(envSliceAfter, fmt.Sprintf("%s=%s", noConsecutiveUnderScoreKey, v))
			dnerrs = dnerrs.Append(DeprecatedNameError{old: noConsecutiveUnderScoreKey, new: k})
		}
	}

	// guarantee that the error value is nil if there are no deprecations
	if !dnerrs.IsEmpty() {
		return envSliceAfter, dnerrs
	}
	return envSliceAfter, nil
}

// ConfigurationToEnvironment converts the plugin configuration values to
// environment variables.
func (p *Plugin) ConfigurationToEnvironment() (*env.Environment, error) {
	envSlice := []string{}
	envPrefix := fmt.Sprintf("BUILDKITE_PLUGIN_%s", formatEnvKey(p.Name()))

	// Append current plugin name
	envSlice = append(envSlice, fmt.Sprintf("BUILDKITE_PLUGIN_NAME=%s", formatEnvKey(p.Name())))

	// Append current plugin configuration as JSON
	configJSON, err := json.Marshal(p.Configuration)
	if err != nil {
		return env.New(), err
	}
	envSlice = append(envSlice, fmt.Sprintf("BUILDKITE_PLUGIN_CONFIGURATION=%s", configJSON))

	for k, v := range p.Configuration {
		configPrefix := fmt.Sprintf("%s_%s", envPrefix, formatEnvKey(k))
		if err := walkConfigValues(configPrefix, v, &envSlice); err != nil {
			return env.New(), err
		}
	}

	envSlice, err = fanOutDeprecatedEnvVarNames(envSlice)
	return env.FromSlice(envSlice), err
}

// Label returns a pretty name for the plugin.
func (p *Plugin) Label() string {
	if p.Version == "" {
		return p.Location
	}
	return p.Location + "#" + p.Version
}

// DisplayName returns a human-friendly name for the plugin suitable for logs.
// Examples:
//   - github.com/org/repo           => repo
//   - github.com/org/repo/.buildkite => repo/.buildkite
//   - file:///path/to/plugin         => plugin (last path element)
func (p *Plugin) DisplayName() string {
	// Filesystem paths: fall back to Name(), which returns the last segment normalized
	if strings.HasPrefix(p.Location, "/") || strings.HasPrefix(p.Location, ".") || strings.Contains(p.Location, "\\") {
		return p.Name()
	}

	host, err := p.constructRepositoryHost()
	if err != nil || host == "" {
		return p.Name()
	}

	// derive repo name from host path
	parts := strings.Split(host, "/")
	repo := parts[len(parts)-1]
	repo = strings.TrimSuffix(repo, ".git")

	// append subdirectory if present
	subdir, err := p.RepositorySubdirectory()
	if err != nil || subdir == "" {
		return repo
	}
	return repo + "/" + subdir
}

func (p *Plugin) constructRepositoryHost() (string, error) {
	if p.Location == "" {
		return "", fmt.Errorf("Missing plugin location")
	}

	parts := strings.Split(p.Location, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("Incomplete plugin path %q", p.Location)
	}

	switch parts[0] {
	case "github.com", "bitbucket.org":
		if len(parts) < 3 {
			return "", fmt.Errorf("Incomplete plugin path %q", p.Location)
		}
		return strings.Join(parts[:3], "/"), nil

	case "gitlab.com":
		if len(parts) < 3 {
			return "", fmt.Errorf("Incomplete plugin path %q", p.Location)
		}
		return strings.Join(parts, "/"), nil

	default:
		repo := []string{}

		for _, p := range parts {
			repo = append(repo, p)

			if strings.HasSuffix(p, ".git") {
				break
			}
		}

		return strings.Join(repo, "/"), nil
	}
}
