package plugin

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/buildkite/agent/env"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
)

func TestCreateFromJSON(t *testing.T) {
	t.Parallel()

	var plugins []*Plugin
	var err error

	plugins, err = CreateFromJSON(`[{"http://github.com/buildkite/plugins/docker-compose#a34fa34":{"container":"app"}}, "github.com/buildkite/plugins/ping#master"]`)
	assert.Check(t, is.Equal(len(plugins), 2))
	assert.Check(t, err)

	assert.Check(t, is.Equal(plugins[0].Location, "github.com/buildkite/plugins/docker-compose"))
	assert.Check(t, is.Equal(plugins[0].Version, "a34fa34"))
	assert.Check(t, is.Equal(plugins[0].Scheme, "http"))
	assert.Check(t, is.DeepEqual(plugins[0].Configuration, map[string]interface{}{"container": "app"}))

	assert.Check(t, is.Equal(plugins[1].Location, "github.com/buildkite/plugins/ping"))
	assert.Check(t, is.Equal(plugins[1].Version, "master"))
	assert.Check(t, is.Equal(plugins[1].Scheme, ""))
	assert.Check(t, is.DeepEqual(plugins[1].Configuration, map[string]interface{}{}))

	plugins, err = CreateFromJSON(`["ssh://git:foo@github.com/buildkite/plugins/docker-compose#a34fa34"]`)
	assert.Check(t, is.Equal(len(plugins), 1))
	assert.Check(t, err)

	assert.Check(t, is.Equal(plugins[0].Location, "github.com/buildkite/plugins/docker-compose"))
	assert.Check(t, is.Equal(plugins[0].Version, "a34fa34"))
	assert.Check(t, is.Equal(plugins[0].Scheme, "ssh"))
	assert.Check(t, is.Equal(plugins[0].Authentication, "git:foo"))

	plugins, err = CreateFromJSON(`blah`)
	assert.Check(t, is.Equal(len(plugins), 0))
	assert.Check(t, err != nil)
	assert.Check(t, is.Equal(err.Error(), "invalid character 'b' looking for beginning of value"))

	plugins, err = CreateFromJSON(`{"foo": "bar"}`)
	assert.Check(t, is.Equal(len(plugins), 0))
	assert.Check(t, err != nil)
	assert.Check(t, is.Equal(err.Error(), "JSON structure was not an array"))

	plugins, err = CreateFromJSON(`["github.com/buildkite/plugins/ping#master#lololo"]`)
	assert.Check(t, is.Equal(len(plugins), 0))
	assert.Check(t, err != nil)
	assert.Check(t, is.Equal(err.Error(), "Too many #'s in \"github.com/buildkite/plugins/ping#master#lololo\""))
}

func TestCreateFromJSONForVendoredPlugins(t *testing.T) {
	t.Parallel()

	plugins, err := CreateFromJSON(`[{"./.buildkite/plugins/llamas":{}}]`)
	assert.Check(t, is.Equal(len(plugins), 1))
	assert.Check(t, err)

	assert.Check(t, is.Equal("./.buildkite/plugins/llamas", plugins[0].Location))

	assert.Check(t, plugins[0].Vendored)
	assert.Check(t, is.Equal("", plugins[0].Version))
	assert.Check(t, is.Equal("", plugins[0].Scheme))
	assert.Check(t, is.DeepEqual(map[string]interface{}{}, plugins[0].Configuration))
}

func TestPluginNameParsedFromLocation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		location     string
		expectedName string
	}{
		{"github.com/buildkite-plugins/docker-compose-buildkite-plugin.git", "docker-compose"},
		{"github.com/buildkite-plugins/docker-compose-buildkite-plugin", "docker-compose"},
		{"github.com/my-org/docker-compose-buildkite-plugin", "docker-compose"},
		{"github.com/buildkite/plugins/docker-compose", "docker-compose"},
		{"~/Development/plugins/test", "test"},
		{"~/Development/plugins/UPPER     CASE_party", "upper-case-party"},
		{"vendor/src/vendored with a space", "vendored-with-a-space"},
		{"./.buildkite/plugins/docker-compose", "docker-compose"},
		{".\\.buildkite\\plugins\\docker-compose", "docker-compose"},
		{".buildkite/plugins/docker-compose", "docker-compose"},
		{"", ""},
	} {
		tc := tc
		t.Run(tc.location, func(tt *testing.T) {
			tt.Parallel()
			plugin := &Plugin{Location: tc.location}
			assert.Check(tt, is.Equal(tc.expectedName, plugin.Name()))
		})
	}
}

func TestIdentifier(t *testing.T) {
	t.Parallel()

	var plugin *Plugin
	var id string
	var err error

	plugin = &Plugin{Location: "github.com/buildkite/plugins/docker-compose/beta#master"}
	id, err = plugin.Identifier()
	assert.Check(t, is.Equal(id, "github-com-buildkite-plugins-docker-compose-beta-master"))
	assert.Check(t, err)

	plugin = &Plugin{Location: "github.com/buildkite/plugins/docker-compose/beta"}
	id, err = plugin.Identifier()
	assert.Check(t, is.Equal(id, "github-com-buildkite-plugins-docker-compose-beta"))
	assert.Check(t, err)

	plugin = &Plugin{Location: "192.168.0.1/foo.git#12341234"}
	id, err = plugin.Identifier()
	assert.Check(t, is.Equal(id, "192-168-0-1-foo-git-12341234"))
	assert.Check(t, err)

	plugin = &Plugin{Location: "/foo/bar/"}
	id, err = plugin.Identifier()
	assert.Check(t, is.Equal(id, "foo-bar"))
	assert.Check(t, err)

	plugin = &Plugin{Location: "./.buildkite/plugins/llamas/"}
	id, err = plugin.Identifier()
	assert.Check(t, is.Equal(id, "buildkite-plugins-llamas"))
	assert.Check(t, err)
}

func TestRepositoryAndSubdirectory(t *testing.T) {
	t.Parallel()

	var plugin *Plugin
	var repo string
	var sub string
	var err error

	plugin = &Plugin{Location: "github.com/buildkite/plugins/docker-compose/beta"}
	repo, err = plugin.Repository()
	assert.Check(t, is.Equal(repo, "https://github.com/buildkite/plugins"))
	assert.Check(t, err)
	sub, err = plugin.RepositorySubdirectory()
	assert.Check(t, is.Equal(sub, "docker-compose/beta"))
	assert.Check(t, err)

	plugin = &Plugin{Location: "github.com/buildkite/test-plugin"}
	repo, err = plugin.Repository()
	assert.Check(t, is.Equal(repo, "https://github.com/buildkite/test-plugin"))
	assert.Check(t, err)
	sub, err = plugin.RepositorySubdirectory()
	assert.Check(t, is.Equal(sub, ""))
	assert.Check(t, err)

	plugin = &Plugin{Location: "github.com/buildkite"}
	repo, err = plugin.Repository()
	assert.Check(t, is.Equal(repo, ""))
	assert.Check(t, err != nil)
	assert.Check(t, is.Equal(err.Error(), `Incomplete github.com path "github.com/buildkite"`))
	sub, err = plugin.RepositorySubdirectory()
	assert.Check(t, is.Equal(sub, ""))
	assert.Check(t, err != nil)
	assert.Check(t, is.Equal(err.Error(), `Incomplete github.com path "github.com/buildkite"`))

	plugin = &Plugin{Location: "bitbucket.org/buildkite"}
	repo, err = plugin.Repository()
	assert.Check(t, is.Equal(repo, ""))
	assert.Check(t, err != nil)
	assert.Check(t, is.Equal(err.Error(), `Incomplete bitbucket.org path "bitbucket.org/buildkite"`))
	sub, err = plugin.RepositorySubdirectory()
	assert.Check(t, is.Equal(sub, ""))
	assert.Check(t, err != nil)
	assert.Check(t, is.Equal(err.Error(), `Incomplete bitbucket.org path "bitbucket.org/buildkite"`))

	plugin = &Plugin{Location: "bitbucket.org/user/project/sub/directory"}
	repo, err = plugin.Repository()
	assert.Check(t, is.Equal(repo, "https://bitbucket.org/user/project"))
	assert.Check(t, err)
	sub, err = plugin.RepositorySubdirectory()
	assert.Check(t, is.Equal(sub, "sub/directory"))
	assert.Check(t, err)

	plugin = &Plugin{Location: "bitbucket.org/user/project/sub/directory", Scheme: "http", Authentication: "foo:bar"}
	repo, err = plugin.Repository()
	assert.Check(t, is.Equal(repo, "http://foo:bar@bitbucket.org/user/project"))
	assert.Check(t, err)
	sub, err = plugin.RepositorySubdirectory()
	assert.Check(t, is.Equal(sub, "sub/directory"))
	assert.Check(t, err)

	plugin = &Plugin{Location: "114.135.234.212/foo.git"}
	repo, err = plugin.Repository()
	assert.Check(t, is.Equal(repo, "https://114.135.234.212/foo.git"))
	assert.Check(t, err)
	sub, err = plugin.RepositorySubdirectory()
	assert.Check(t, is.Equal(sub, ""))
	assert.Check(t, err)

	plugin = &Plugin{Location: "github.com/buildkite/plugins/docker-compose/beta"}
	repo, err = plugin.Repository()
	assert.Check(t, is.Equal(repo, "https://github.com/buildkite/plugins"))
	assert.Check(t, err)
	sub, err = plugin.RepositorySubdirectory()
	assert.Check(t, is.Equal(sub, "docker-compose/beta"))
	assert.Check(t, err)

	plugin = &Plugin{Location: "/Users/keithpitt/Development/plugins.git/test-plugin"}
	repo, err = plugin.Repository()
	assert.Check(t, is.Equal(repo, "/Users/keithpitt/Development/plugins.git"))
	assert.Check(t, err)
	sub, err = plugin.RepositorySubdirectory()
	assert.Check(t, is.Equal(sub, "test-plugin"))
	assert.Check(t, err)

	plugin = &Plugin{Location: ""}
	repo, err = plugin.Repository()
	assert.Check(t, is.Equal(repo, ""))
	assert.Check(t, err != nil)
	assert.Check(t, is.Equal(err.Error(), "Missing plugin location"))
}

func TestConfigurationToEnvironment(t *testing.T) {
	t.Parallel()

	var envMap *env.Environment
	var err error

	envMap, err = pluginEnvFromConfig(t, `{ "config-key": 42 }`)
	assert.Check(t, err)
	assert.Check(t, is.DeepEqual([]string{"BUILDKITE_PLUGIN_DOCKER_COMPOSE_CONFIG_KEY=42"}, envMap.ToSlice()))

	envMap, err = pluginEnvFromConfig(t, `{ "container": "app", "some-other-setting": "else right here" }`)
	assert.Check(t, err)
	assert.Check(t, is.DeepEqual([]string{
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_CONTAINER=app",
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_SOME_OTHER_SETTING=else right here"},
		envMap.ToSlice()))

	envMap, err = pluginEnvFromConfig(t, `{ "and _ with a    - number": 12 }`)
	assert.Check(t, err)
	assert.Check(t, is.DeepEqual([]string{"BUILDKITE_PLUGIN_DOCKER_COMPOSE_AND_WITH_A_NUMBER=12"}, envMap.ToSlice()))

	envMap, err = pluginEnvFromConfig(t, `{ "bool-true-key": true, "bool-false-key": false }`)
	assert.Check(t, err)
	assert.Check(t, is.DeepEqual([]string{
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_BOOL_FALSE_KEY=false",
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_BOOL_TRUE_KEY=true"},
		envMap.ToSlice()))

	envMap, err = pluginEnvFromConfig(t, `{ "array-key": [ "array-val-1", "array-val-2" ] }`)
	assert.Check(t, err)
	assert.Check(t, is.DeepEqual([]string{
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0=array-val-1",
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_1=array-val-2"},
		envMap.ToSlice()))

	envMap, err = pluginEnvFromConfig(t, `{ "array-key": [ 42, 43, 44 ] }`)
	assert.Check(t, err)
	assert.Check(t, is.DeepEqual([]string{
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0=42",
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_1=43",
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_2=44"},
		envMap.ToSlice()))

	envMap, err = pluginEnvFromConfig(t, `{ "array-key": [ 42, 43, "foo" ] }`)
	assert.Check(t, err)
	assert.Check(t, is.DeepEqual([]string{
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0=42",
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_1=43",
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_2=foo"},
		envMap.ToSlice()))

	envMap, err = pluginEnvFromConfig(t, `{ "array-key": [ { "subkey": "subval" } ] }`)
	assert.Check(t, err)
	assert.Check(t, is.DeepEqual([]string{
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0_SUBKEY=subval"},
		envMap.ToSlice()))

	envMap, err = pluginEnvFromConfig(t, `{ "array-key": [ { "subkey": [1, 2, "llamas"] } ] }`)
	assert.Check(t, err)
	assert.Check(t, is.DeepEqual([]string{
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0_SUBKEY_0=1",
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0_SUBKEY_1=2",
		"BUILDKITE_PLUGIN_DOCKER_COMPOSE_ARRAY_KEY_0_SUBKEY_2=llamas",
	}, envMap.ToSlice()))
}

func pluginEnvFromConfig(t *testing.T, configJson string) (*env.Environment, error) {
	var config map[string]interface{}

	json.Unmarshal([]byte(configJson), &config)

	jsonString := fmt.Sprintf(`[ { "%s": %s } ]`, "github.com/buildkite-plugins/docker-compose-buildkite-plugin", configJson)

	plugins, err := CreateFromJSON(jsonString)

	assert.Check(t, err)
	assert.Check(t, is.Equal(1, len(plugins)))

	return plugins[0].ConfigurationToEnvironment()
}
