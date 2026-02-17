package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCreateFromJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		jsonText string
		plugins  []*Plugin
	}{
		{
			`[{"https://github.com/buildkite-plugins/docker-compose#a34fa34":{"container":"app"}}]`,
			[]*Plugin{{
				Location:      "github.com/buildkite-plugins/docker-compose",
				Version:       "a34fa34",
				Scheme:        "https",
				Configuration: map[string]any{"container": "app"},
			}},
		},
		{
			`[{"github.com/buildkite-plugins/docker-compose#a34fa34":{}}]`,
			[]*Plugin{{
				Location:      "github.com/buildkite-plugins/docker-compose",
				Version:       "a34fa34",
				Scheme:        "",
				Configuration: map[string]any{},
			}},
		},
		{
			`[{"http://github.com/buildkite-plugins/docker-compose#a34fa34":{}}]`,
			[]*Plugin{{
				Location:      "github.com/buildkite-plugins/docker-compose",
				Version:       "a34fa34",
				Scheme:        "http",
				Configuration: map[string]any{},
			}},
		},
		{
			`[{"https://gitlab.example.com/path/to/repo#main":{}}]`,
			[]*Plugin{{
				Location:      "gitlab.example.com/path/to/repo",
				Version:       "main",
				Scheme:        "https",
				Configuration: map[string]any{},
			}},
		},
		{
			`[{"https://gitlab.com/group/team/path/to/repo#main":{}}]`,
			[]*Plugin{{
				Location:      "gitlab.com/group/team/path/to/repo",
				Version:       "main",
				Scheme:        "https",
				Configuration: map[string]any{},
			}},
		},
		{
			`["ssh://git:foo@github.com/buildkite-plugins/docker-compose#a34fa34"]`,
			[]*Plugin{{
				Location:       "github.com/buildkite-plugins/docker-compose",
				Version:        "a34fa34",
				Scheme:         "ssh",
				Authentication: "git:foo",
				Configuration:  map[string]any{},
			}},
		},
		{
			`["file://github.com/buildkite-plugins/docker-compose#a34fa34"]`,
			[]*Plugin{{
				Location:      "github.com/buildkite-plugins/docker-compose",
				Version:       "a34fa34",
				Scheme:        "file",
				Configuration: map[string]any{},
			}},
		},
		{
			`["github.com/buildkite-plugins/fake-plugin#main"]`,
			[]*Plugin{{
				Location:      "github.com/buildkite-plugins/fake-plugin",
				Version:       "main",
				Scheme:        "",
				Configuration: map[string]any{},
			}},
		},
		{
			`[{"./.buildkite/plugins/llamas":{}}]`,
			[]*Plugin{{
				Location:      "./.buildkite/plugins/llamas",
				Scheme:        "",
				Vendored:      true,
				Configuration: map[string]any{},
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.jsonText, func(t *testing.T) {
			t.Parallel()

			plugins, err := CreateFromJSON(tc.jsonText)
			if err != nil {
				t.Errorf("CreateFromJSON(%q) error = %v", tc.jsonText, err)
			}

			if diff := cmp.Diff(tc.plugins, plugins); diff != "" {
				t.Errorf("CreateFromJSON(%q) diff (-got +want)\n%s", tc.jsonText, diff)
			}
		})
	}
}

func TestCreateFromJSONFailsOnParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		jsonText string
		err      string
	}{
		{
			"blah",
			"invalid character 'b' looking for beginning of value",
		},
		{
			`{"foo": "bar"}`,
			"JSON structure was not an array",
		},
		{
			`["github.com/buildkite-plugins/ping#main#lololo"]`,
			"Too many '#'s in \"github.com/buildkite-plugins/ping#main#lololo\"",
		},
	}

	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()

			plugins, err := CreateFromJSON(tc.jsonText)
			if err.Error() != tc.err {
				// TODO: Testing error strings is fragile - replace with a more semantic test.
				t.Errorf("CreateFromJSON(%q) error = %q, want %q", tc.jsonText, err, tc.err)
			}
			if got, want := len(plugins), 0; got != want {
				t.Errorf("len(CreateFromJSON(%q)) = %d, want %d", tc.jsonText, got, want)
			}
		})
	}
}

func TestPluginNameParsedFromLocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		location string
		wantName string
	}{
		{
			location: "github.com/buildkite-plugins/docker-compose-buildkite-plugin.git",
			wantName: "docker-compose",
		},
		{
			location: "github.com/buildkite-plugins/docker-compose-buildkite-plugin",
			wantName: "docker-compose",
		},
		{
			location: "github.com/my-org/docker-compose-buildkite-plugin",
			wantName: "docker-compose",
		},
		{
			location: "github.com/buildkite/plugins/docker-compose",
			wantName: "docker-compose",
		},
		{
			location: "~/Development/plugins/test",
			wantName: "test",
		},
		{
			location: "~/Development/plugins/UPPER     CASE_party",
			wantName: "upper-case-party",
		},
		{
			location: "vendor/src/vendored with a space",
			wantName: "vendored-with-a-space",
		},
		{
			location: "vendor/src/vendored-with-a-slash/",
			wantName: "vendored-with-a-slash",
		},
		{
			location: "vendor/src/vendored-with-two-slash//",
			wantName: "vendored-with-two-slash",
		},
		{
			location: "./.buildkite/plugins/docker-compose",
			wantName: "docker-compose",
		},
		{
			location: ".\\.buildkite\\plugins\\docker-compose",
			wantName: "docker-compose",
		},
		{
			location: ".buildkite/plugins/docker-compose",
			wantName: "docker-compose",
		},
		{
			location: "",
			wantName: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.location, func(t *testing.T) {
			t.Parallel()
			plugin := &Plugin{Location: tc.location}
			if got, want := plugin.Name(), tc.wantName; got != want {
				t.Errorf("Plugin(Location: %q).Name() = %q, want %q", tc.location, got, want)
			}
		})
	}
}

func TestIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		location, wantID string
	}{
		{
			location: "github.com/buildkite/plugins/docker-compose/beta#main",
			wantID:   "github-com-buildkite-plugins-docker-compose-beta-main",
		},
		{
			location: "github.com/buildkite/plugins/docker-compose/beta",
			wantID:   "github-com-buildkite-plugins-docker-compose-beta",
		},
		{
			location: "192.168.0.1/foo.git#12341234",
			wantID:   "192-168-0-1-foo-git-12341234",
		},
		{
			location: "/foo/bar/",
			wantID:   "foo-bar",
		},
		{
			location: "./.buildkite/plugins/llamas/",
			wantID:   "buildkite-plugins-llamas",
		},
	}

	for _, tc := range tests {
		t.Run(tc.location, func(t *testing.T) {
			t.Parallel()
			plugin := &Plugin{Location: tc.location}
			id, err := plugin.Identifier()
			if err != nil {
				t.Errorf("Plugin{Location: %q}.Identifier() error = %v", tc.location, err)
			}
			if got, want := id, tc.wantID; got != want {
				t.Errorf("Plugin{Location: %q}.Identifier() = %q, want %q", tc.location, got, want)
			}
		})
	}
}

func TestRepositoryAndSubdirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		plugin            *Plugin
		wantRepo, wantSub string
	}{
		{
			plugin:   &Plugin{Location: "github.com/buildkite/plugins/docker-compose/beta"},
			wantRepo: "https://github.com/buildkite/plugins",
			wantSub:  "docker-compose/beta",
		},
		{
			plugin:   &Plugin{Location: "github.com/buildkite/test-plugin"},
			wantRepo: "https://github.com/buildkite/test-plugin",
			wantSub:  "",
		},
		{
			plugin:   &Plugin{Location: "bitbucket.org/user/project/sub/directory"},
			wantRepo: "https://bitbucket.org/user/project",
			wantSub:  "sub/directory",
		},
		{
			plugin:   &Plugin{Location: "bitbucket.org/user/project/sub/directory", Scheme: "http", Authentication: "foo:bar"},
			wantRepo: "http://foo:bar@bitbucket.org/user/project",
			wantSub:  "sub/directory",
		},
		{
			plugin:   &Plugin{Location: "114.135.234.212/foo.git"},
			wantRepo: "https://114.135.234.212/foo.git",
			wantSub:  "",
		},
		{
			plugin:   &Plugin{Location: "github.com/buildkite/plugins/docker-compose/beta"},
			wantRepo: "https://github.com/buildkite/plugins",
			wantSub:  "docker-compose/beta",
		},
		{
			plugin:   &Plugin{Location: "/Users/keithpitt/Development/plugins.git/test-plugin"},
			wantRepo: "/Users/keithpitt/Development/plugins.git",
			wantSub:  "test-plugin",
		},
	}

	for _, tc := range tests {
		t.Run(tc.plugin.Label(), func(t *testing.T) {
			t.Parallel()
			repo, err := tc.plugin.Repository()
			if err != nil {
				t.Errorf("plugin.Repository() error = %v", err)
			}
			if got, want := repo, tc.wantRepo; got != want {
				t.Errorf("plugin.Repository() = %q, want %q", got, want)
			}
			sub, err := tc.plugin.RepositorySubdirectory()
			if err != nil {
				t.Errorf("plugin.RepositorySubdirectory() error = %v", err)
			}
			if got, want := sub, tc.wantSub; got != want {
				t.Errorf("plugin.RepositorySubdirectory() = %q, want %q", got, want)
			}
		})
	}
}

func TestRespositoryAndSubdirectoryErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		location string
		wantErr  string
	}{
		{
			location: "github.com/buildkite",
			wantErr:  `Incomplete plugin path "github.com/buildkite"`,
		},
		{
			location: "bitbucket.org/buildkite",
			wantErr:  `Incomplete plugin path "bitbucket.org/buildkite"`,
		},
		{
			location: "",
			wantErr:  "Missing plugin location",
		},
	}
	for _, tc := range tests {
		t.Run(tc.location, func(t *testing.T) {
			t.Parallel()

			plugin := &Plugin{Location: tc.location}
			_, err := plugin.Repository()
			if got, want := err.Error(), tc.wantErr; got != want {
				// TODO: Testing error strings is fragile - replace with a more semantic test.
				t.Errorf("plugin.Repository() error = %q, want %q", got, want)
			}
			_, err = plugin.RepositorySubdirectory()
			if got, want := err.Error(), tc.wantErr; got != want {
				// TODO: Testing error strings is fragile - replace with a more semantic test.
				t.Errorf("plugin.RepositorySubdirectory() error = %q, want %q", got, want)
			}
		})
	}
}

func TestConfigurationToEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		configJSON  string
		wantEnvMap  map[string]string
		expectedErr error
	}{
		{
			configJSON: `{ "config-key": 42 }`,
			wantEnvMap: map[string]string{
				"BUILDKITE_PLUGIN_CONFIGURATION":             `{"config-key":42}`,
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_CONFIG_KEY": "42",
				"BUILDKITE_PLUGIN_NAME":                      "DOCKER_COMPOSE",
			},
		},
		{
			configJSON: `{ "container": "app", "some-other-setting": "else right here" }`,
			wantEnvMap: map[string]string{
				"BUILDKITE_PLUGIN_CONFIGURATION":                     `{"container":"app","some-other-setting":"else right here"}`,
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_CONTAINER":          "app",
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_SOME_OTHER_SETTING": "else right here",
				"BUILDKITE_PLUGIN_NAME":                              "DOCKER_COMPOSE",
			},
		},
		{
			configJSON: `{ "and _ with a    - number": 12 }`,
			wantEnvMap: map[string]string{
				"BUILDKITE_PLUGIN_CONFIGURATION":                           `{"and _ with a    - number":12}`,
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_AND_WITH_A_NUMBER":        "12",
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_AND___WITH_A______NUMBER": "12",
				"BUILDKITE_PLUGIN_NAME":                                    "DOCKER_COMPOSE",
			},
			expectedErr: (&DeprecatedNameErrors{}).Append(
				DeprecatedNameError{
					old: "BUILDKITE_PLUGIN_DOCKER_COMPOSE_AND_WITH_A_NUMBER",
					new: "BUILDKITE_PLUGIN_DOCKER_COMPOSE_AND___WITH_A______NUMBER",
				},
			),
		},
		{
			configJSON: `{ "and _ with a    - number": 12, "A - B": 13 }`,
			wantEnvMap: map[string]string{
				"BUILDKITE_PLUGIN_CONFIGURATION":                           `{"A - B":13,"and _ with a    - number":12}`,
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_AND_WITH_A_NUMBER":        "12",
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_AND___WITH_A______NUMBER": "12",
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_A_B":                      "13",
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_A___B":                    "13",
				"BUILDKITE_PLUGIN_NAME":                                    "DOCKER_COMPOSE",
			},
			expectedErr: (&DeprecatedNameErrors{}).Append(
				DeprecatedNameError{
					old: "BUILDKITE_PLUGIN_DOCKER_COMPOSE_AND_WITH_A_NUMBER",
					new: "BUILDKITE_PLUGIN_DOCKER_COMPOSE_AND___WITH_A______NUMBER",
				},
				DeprecatedNameError{
					old: "BUILDKITE_PLUGIN_DOCKER_COMPOSE_A_B",
					new: "BUILDKITE_PLUGIN_DOCKER_COMPOSE_A___B",
				},
			),
		},
		{
			configJSON: `{ "bool-true-key": true, "bool-false-key": false }`,
			wantEnvMap: map[string]string{
				"BUILDKITE_PLUGIN_CONFIGURATION":                 `{"bool-false-key":false,"bool-true-key":true}`,
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_BOOL_FALSE_KEY": "false",
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_BOOL_TRUE_KEY":  "true",
				"BUILDKITE_PLUGIN_NAME":                          "DOCKER_COMPOSE",
			},
		},
		{
			configJSON: `{ "array-key": [ "array-val-1", "array-val-2" ] }`,
			wantEnvMap: map[string]string{
				"BUILDKITE_PLUGIN_CONFIGURATION":              `{"array-key":["array-val-1","array-val-2"]}`,
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0": "array-val-1",
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_1": "array-val-2",
				"BUILDKITE_PLUGIN_NAME":                       "DOCKER_COMPOSE",
			},
		},
		{
			configJSON: `{ "array-key": [ 42, 43, 44 ] }`,
			wantEnvMap: map[string]string{
				"BUILDKITE_PLUGIN_CONFIGURATION":              `{"array-key":[42,43,44]}`,
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0": "42",
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_1": "43",
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_2": "44",
				"BUILDKITE_PLUGIN_NAME":                       "DOCKER_COMPOSE",
			},
		},
		{
			configJSON: `{ "array-key": [ 42, 43, "foo" ] }`,
			wantEnvMap: map[string]string{
				"BUILDKITE_PLUGIN_CONFIGURATION":              `{"array-key":[42,43,"foo"]}`,
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0": "42",
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_1": "43",
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_2": "foo",
				"BUILDKITE_PLUGIN_NAME":                       "DOCKER_COMPOSE",
			},
		},
		{
			configJSON: `{ "array-key": [ { "subkey": "subval" } ] }`,
			wantEnvMap: map[string]string{
				"BUILDKITE_PLUGIN_CONFIGURATION":                     `{"array-key":[{"subkey":"subval"}]}`,
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0_SUBKEY": "subval",
				"BUILDKITE_PLUGIN_NAME":                              "DOCKER_COMPOSE",
			},
		},
		{
			configJSON: `{ "array-key": [ { "subkey": [1, 2, "llamas"] } ] }`,
			wantEnvMap: map[string]string{
				"BUILDKITE_PLUGIN_CONFIGURATION":                       `{"array-key":[{"subkey":[1,2,"llamas"]}]}`,
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0_SUBKEY_0": "1",
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0_SUBKEY_1": "2",
				"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0_SUBKEY_2": "llamas",
				"BUILDKITE_PLUGIN_NAME":                                "DOCKER_COMPOSE",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.configJSON, func(t *testing.T) {
			t.Parallel()
			plugin, err := pluginFromConfig(tc.configJSON)
			if err != nil {
				t.Fatalf("pluginFromConfig(%q) error = %v", tc.configJSON, err)
			}
			env, err := plugin.ConfigurationToEnvironment()
			if !errors.Is(err, tc.expectedErr) {
				t.Errorf("plugin.ConfigurationToEnvironment() error got:\n%v\nwant:\n%v", err, tc.expectedErr)
			}
			envMap := env.Dump()
			if diff := cmp.Diff(envMap, tc.wantEnvMap); diff != "" {
				t.Errorf("plugin.ConfigurationToEnvironment() envMap diff (-got +want)\n%s", diff)
			}
		})
	}
}

func pluginFromConfig(configJSON string) (*Plugin, error) {
	var config map[string]any

	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, err
	}

	jsonString := fmt.Sprintf(
		`[ { "%s": %s } ]`,
		"github.com/buildkite-plugins/docker-compose-buildkite-plugin",
		configJSON,
	)

	plugins, err := CreateFromJSON(jsonString)
	if err != nil {
		return nil, err
	}
	if len(plugins) != 1 {
		return nil, fmt.Errorf("parsed wrong number of plugins [%d != 1]", len(plugins))
	}

	return plugins[0], nil
}

func TestConfigurationToEnvironment_DuplicatePlugin(t *testing.T) {
	t.Parallel()

	// Ensure on duplicate plugin definition, each plugin gets its respective config exported
	plugins, err := duplicatePluginFromConfig(`{ "config-key": 41 }`, `{ "second-ref-key": 42 }`)
	if err != nil {
		t.Fatalf("duplicatePluginFromConfig({config-key:41},{second-ref-key:42}) error = %v", err)
	}

	envMap1, err := plugins[0].ConfigurationToEnvironment()
	if err != nil {
		t.Errorf("plugins[0].ConfigurationToEnvironment() error = %v", err)
	}
	wantEnv1 := map[string]string{
		"BUILDKITE_PLUGIN_CONFIGURATION":             `{"config-key":41}`,
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_CONFIG_KEY": "41",
		"BUILDKITE_PLUGIN_NAME":                      "DOCKER_COMPOSE",
	}
	if diff := cmp.Diff(envMap1.Dump(), wantEnv1); diff != "" {
		t.Errorf("plugins[0].ConfigurationToEnvironment() envMap diff (-got +want)\n%s", diff)
	}

	envMap2, err := plugins[1].ConfigurationToEnvironment()
	if err != nil {
		t.Errorf("plugins[1].ConfigurationToEnvironment() error = %v", err)
	}

	wantEnv2 := map[string]string{
		"BUILDKITE_PLUGIN_CONFIGURATION":                 `{"second-ref-key":42}`,
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_SECOND_REF_KEY": "42",
		"BUILDKITE_PLUGIN_NAME":                          "DOCKER_COMPOSE",
	}

	if diff := cmp.Diff(envMap2.Dump(), wantEnv2); diff != "" {
		t.Errorf("plugins[0].ConfigurationToEnvironment() envMap diff (-got +want)\n%s", diff)
	}
}

func duplicatePluginFromConfig(cfgJSON1, cfgJSON2 string) ([]*Plugin, error) {
	var config1, config2 map[string]any

	if err := json.Unmarshal([]byte(cfgJSON1), &config1); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(cfgJSON1), &config2); err != nil {
		return nil, err
	}

	jsonString := fmt.Sprintf(
		`[ { "%s": %s }, { "%s": %s } ]`,
		"github.com/buildkite-plugins/docker-compose-buildkite-plugin",
		cfgJSON1,
		"github.com/buildkite-plugins/docker-compose-buildkite-plugin",
		cfgJSON2,
	)

	plugins, err := CreateFromJSON(jsonString)
	if err != nil {
		return nil, err
	}
	if len(plugins) != 2 {
		return nil, fmt.Errorf("parsed wrong number of plugins [%d != 2]", len(plugins))
	}

	return plugins, nil
}

func TestZipPluginParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		jsonText string
		plugin   *Plugin
	}{
		{
			`["zip+https://example.com/plugins/my-plugin.zip"]`,
			&Plugin{
				Location:      "example.com/plugins/my-plugin.zip",
				Scheme:        "zip+https",
				Configuration: map[string]any{},
			},
		},
		{
			`["zip+https://example.com/plugins/my-plugin.zip#v1.0.0"]`,
			&Plugin{
				Location:      "example.com/plugins/my-plugin.zip",
				Version:       "v1.0.0",
				Scheme:        "zip+https",
				Configuration: map[string]any{},
			},
		},
		{
			`["zip+https://example.com/plugins/my-plugin.zip#sha256:abc123def456"]`,
			&Plugin{
				Location:      "example.com/plugins/my-plugin.zip",
				Version:       "sha256:abc123def456",
				Scheme:        "zip+https",
				Configuration: map[string]any{},
			},
		},
		{
			`["zip+http://example.com/plugins/my-plugin.zip"]`,
			&Plugin{
				Location:      "example.com/plugins/my-plugin.zip",
				Scheme:        "zip+http",
				Configuration: map[string]any{},
			},
		},
		{
			`["zip+file:///opt/plugins/docker-compose.zip"]`,
			&Plugin{
				Location:      "/opt/plugins/docker-compose.zip",
				Scheme:        "zip+file",
				Configuration: map[string]any{},
			},
		},
		{
			`["zip+https://user:pass@example.com/plugin.zip"]`,
			&Plugin{
				Location:       "example.com/plugin.zip",
				Scheme:         "zip+https",
				Authentication: "user:pass",
				Configuration:  map[string]any{},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.jsonText, func(t *testing.T) {
			t.Parallel()

			plugins, err := CreateFromJSON(tc.jsonText)
			if err != nil {
				t.Errorf("CreateFromJSON(%q) error = %v", tc.jsonText, err)
			}

			if len(plugins) != 1 {
				t.Fatalf("CreateFromJSON(%q) returned %d plugins, want 1", tc.jsonText, len(plugins))
			}

			if diff := cmp.Diff(tc.plugin, plugins[0]); diff != "" {
				t.Errorf("CreateFromJSON(%q) diff (-want +got)\n%s", tc.jsonText, diff)
			}
		})
	}
}

func TestIsZipPlugin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scheme     string
		wantIsZip  bool
		wantBase   string
	}{
		{
			scheme:    "zip+https",
			wantIsZip: true,
			wantBase:  "https",
		},
		{
			scheme:    "zip+http",
			wantIsZip: true,
			wantBase:  "http",
		},
		{
			scheme:    "zip+file",
			wantIsZip: true,
			wantBase:  "file",
		},
		{
			scheme:    "https",
			wantIsZip: false,
			wantBase:  "https",
		},
		{
			scheme:    "ssh",
			wantIsZip: false,
			wantBase:  "ssh",
		},
		{
			scheme:    "",
			wantIsZip: false,
			wantBase:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.scheme, func(t *testing.T) {
			t.Parallel()

			p := &Plugin{Scheme: tc.scheme}

			if got := p.IsZipPlugin(); got != tc.wantIsZip {
				t.Errorf("Plugin{Scheme: %q}.IsZipPlugin() = %v, want %v", tc.scheme, got, tc.wantIsZip)
			}

			if got := p.ZipBaseScheme(); got != tc.wantBase {
				t.Errorf("Plugin{Scheme: %q}.ZipBaseScheme() = %q, want %q", tc.scheme, got, tc.wantBase)
			}
		})
	}
}

func TestZipPluginIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		location, version, wantID string
	}{
		{
			location: "example.com/plugins/my-plugin.zip",
			version:  "v1.0.0",
			wantID:   "example-com-plugins-my-plugin-zip-v1-0-0",
		},
		{
			location: "example.com/plugins/my-plugin.zip",
			version:  "sha256:abc123",
			wantID:   "example-com-plugins-my-plugin-zip-sha256-abc123",
		},
		{
			location: "/opt/plugins/docker-compose.zip",
			wantID:   "opt-plugins-docker-compose-zip",
		},
	}

	for _, tc := range tests {
		t.Run(tc.location, func(t *testing.T) {
			t.Parallel()

			p := &Plugin{
				Location: tc.location,
				Version:  tc.version,
			}

			id, err := p.Identifier()
			if err != nil {
				t.Errorf("Plugin.Identifier() error = %v", err)
			}

			if got, want := id, tc.wantID; got != want {
				t.Errorf("Plugin{Location: %q, Version: %q}.Identifier() = %q, want %q",
					tc.location, tc.version, got, want)
			}
		})
	}
}
