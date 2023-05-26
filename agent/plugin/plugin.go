// Package plugin provides types for managing agent plugins.
//
// It is intended for internal use by buildkite-agent only.
package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/buildkite/agent/v3/env"
)

var (
	nonIDCharacterRE        = regexp.MustCompile(`[^a-zA-Z0-9]`)
	consecutiveHyphenRE     = regexp.MustCompile(`-+`)
	dashOrSpace             = regexp.MustCompile(`-|\s`)
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
		return nil, fmt.Errorf("Too many #'s in \"%s\"", location)
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

// Name returns the name of the plugin.
func (p *Plugin) Name() string {
	if p.Location == "" {
		return ""
	}
	// for filepaths, we can get windows backslashes, so we normalize them
	location := strings.Replace(p.Location, "\\", "/", -1)

	// Grab the last part of the location
	parts := strings.Split(location, "/")
	name := parts[len(parts)-1]

	// Clean up the name
	name = strings.ToLower(name)
	name = whitespaceRE.ReplaceAllString(name, " ")
	name = nonIDCharacterRE.ReplaceAllString(name, "-")
	name = strings.Replace(name, "-buildkite-plugin-git", "", -1)
	name = strings.Replace(name, "-buildkite-plugin", "", -1)

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

// Repository returns the repository host where the code is stored.
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
func (p *Plugin) RepositorySubdirectory() (string, error) {
	repository, err := p.constructRepositoryHost()
	if err != nil {
		return "", err
	}

	dir := strings.TrimPrefix(p.Location, repository)

	return strings.TrimPrefix(dir, "/"), nil
}

// formatEnvKey converts strings into an ENV key friendly format
func formatEnvKey(key string) string {
	newKey := strings.ToUpper(key)
	return dashOrSpace.ReplaceAllString(newKey, "_")
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

// DeprecatedNameErrors contains an aggregation of DeprecatedNameError
type DeprecatedNameErrors struct {
	errs []DeprecatedNameError
}

// Errors returns the underlying slice in sorted order
func (e *DeprecatedNameErrors) Errors() []DeprecatedNameError {
	if e == nil {
		return []DeprecatedNameError{}
	}

	errs := e.errs
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].old == errs[j].old {
			return errs[i].new < errs[j].new
		}
		return errs[i].old < errs[j].old
	})

	return errs
}

// Len is the length of the underlying slice or 0 if nil
func (e *DeprecatedNameErrors) Len() int {
	if e == nil {
		return 0
	}
	return len(e.errs)
}

// Error returns each error message in the underlying slice on new line
func (e *DeprecatedNameErrors) Error() string {
	builder := strings.Builder{}
	for i, err := range e.errs {
		_, _ = builder.WriteString(err.Error())
		if i < len(e.errs)-1 {
			_, _ = builder.WriteRune('\n')
		}
	}

	return builder.String()
}

// Append DeprecatedNameError to the underlying slice and return the reciver
// returning the reveiver is necessary to support appending to nil. So this
// should be used just like the builtin append function
func (e *DeprecatedNameErrors) Append(errs ...DeprecatedNameError) *DeprecatedNameErrors {
	if e == nil {
		return &DeprecatedNameErrors{errs: errs}
	}

	e.errs = append(e.errs, errs...)

	return e
}

// Is returns true if and only if a error that is wrapped in target
// has the same underlying slice as the receiver, regardless of order.
func (e *DeprecatedNameErrors) Is(target error) bool {
	if e == nil {
		return target == nil
	}

	var targetErr *DeprecatedNameErrors
	if !errors.As(target, &targetErr) {
		return false
	}

	dict := make(map[DeprecatedNameError]int, len(e.errs))
	for _, err := range e.errs {
		if c, exists := dict[err]; !exists {
			dict[err] = 1
		} else {
			dict[err] = c + 1
		}
	}

	for _, err := range targetErr.errs {
		c, exists := dict[err]
		if !exists {
			return false
		}
		dict[err] = c - 1
	}

	for _, v := range dict {
		if v != 0 {
			return false
		}
	}

	return true
}

// DeprecatedNameError contains information about environment variable names that
// are deprecated. Both the deprecated name and its replacement are held
type DeprecatedNameError struct {
	old string
	new string
}

func (e *DeprecatedNameError) Error() string {
	return fmt.Sprintf(" deprecated: %q\nreplacement: %q\n", e.old, e.new)
}

func (e *DeprecatedNameError) Is(target error) bool {
	if e == nil {
		return target == nil
	}

	var targetErr *DeprecatedNameError
	if !errors.As(target, &targetErr) {
		return false
	}

	return e.old == targetErr.old && e.new == targetErr.new
}

// The input should be a slice of Env Variables of the form `k=v` where `k` is the variable name
// and `v` is its value. If it can be determined that any of the env variables names is part of a
// deprecation, either as the deprecated variable name or its replacement, append both to the
// returned slice, and also append a deprecation error to the error value. If there are no
// deprecations, the error value is guaranteed to be nil.
func fanOutDeprectatedEnvVarNames(envSlice []string) ([]string, error) {
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
	if dnerrs.Len() != 0 {
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

	envSlice, err = fanOutDeprectatedEnvVarNames(envSlice)
	return env.FromSlice(envSlice), err
}

// Label returns a pretty name for the plugin.
func (p *Plugin) Label() string {
	if p.Version == "" {
		return p.Location
	}
	return p.Location + "#" + p.Version
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
