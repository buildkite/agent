package agent

import (
	"testing"

	"github.com/buildkite/agent/shell"
	"github.com/stretchr/testify/assert"
)

func TestCreatePluginsFromJSON(t *testing.T) {
	var plugins []*Plugin
	var err error

	plugins, err = CreatePluginsFromJSON(`[{"github.com/buildkite/plugins/docker-compose#a34fa34":{"container":"app"}}, "github.com/buildkite/plugins/ping#master"]`)
	assert.Equal(t, len(plugins), 2)
	assert.Nil(t, err)

	assert.Equal(t, plugins[0].Location, "github.com/buildkite/plugins/docker-compose")
	assert.Equal(t, plugins[0].Version, "a34fa34")
	assert.Equal(t, plugins[0].Configuration, map[string]interface{}{"container": "app"})

	assert.Equal(t, plugins[1].Location, "github.com/buildkite/plugins/ping")
	assert.Equal(t, plugins[1].Version, "master")
	assert.Equal(t, plugins[1].Configuration, map[string]interface{}{})

	plugins, err = CreatePluginsFromJSON(`blah`)
	assert.Equal(t, len(plugins), 0)
	assert.NotNil(t, err)
	assert.Equal(t, err.Error(), "invalid character 'b' looking for beginning of value")

	plugins, err = CreatePluginsFromJSON(`{"foo": "bar"}`)
	assert.Equal(t, len(plugins), 0)
	assert.NotNil(t, err)
	assert.Equal(t, err.Error(), "JSON structure was not an array")

	plugins, err = CreatePluginsFromJSON(`["github.com/buildkite/plugins/ping#master#lololo"]`)
	assert.Equal(t, len(plugins), 0)
	assert.NotNil(t, err)
	assert.Equal(t, err.Error(), "Too many #'s in \"github.com/buildkite/plugins/ping#master#lololo\"")
}

func TestPluginName(t *testing.T) {
	var plugin *Plugin

	plugin = &Plugin{Location: "github.com/buildkite/plugins/docker-compose"}
	assert.Equal(t, plugin.Name(), "docker-compose")

	plugin = &Plugin{Location: "github.com/buildkite/my-plugin"}
	assert.Equal(t, plugin.Name(), "my-plugin")

	plugin = &Plugin{Location: "~/Development/plugins/test"}
	assert.Equal(t, plugin.Name(), "test")

	plugin = &Plugin{Location: "~/Development/plugins/UPPER     CASE_party"}
	assert.Equal(t, plugin.Name(), "upper-case-party")

	plugin = &Plugin{Location: "vendor/src/vendored with a space"}
	assert.Equal(t, plugin.Name(), "vendored-with-a-space")

	plugin = &Plugin{Location: ""}
	assert.Equal(t, plugin.Name(), "")
}

func TestIdentifier(t *testing.T) {
	var plugin *Plugin
	var id string
	var err error

	plugin = &Plugin{Location: "github.com/buildkite/plugins/docker-compose/beta#master"}
	id, err = plugin.Identifier()
	assert.Equal(t, id, "github-com-buildkite-plugins-docker-compose-beta-master")
	assert.Nil(t, err)

	plugin = &Plugin{Location: "github.com/buildkite/plugins/docker-compose/beta"}
	id, err = plugin.Identifier()
	assert.Equal(t, id, "github-com-buildkite-plugins-docker-compose-beta")
	assert.Nil(t, err)

	plugin = &Plugin{Location: "192.168.0.1/foo.git#12341234"}
	id, err = plugin.Identifier()
	assert.Equal(t, id, "192-168-0-1-foo-git-12341234")
	assert.Nil(t, err)

	plugin = &Plugin{Location: "/foo/bar/"}
	id, err = plugin.Identifier()
	assert.Equal(t, id, "foo-bar")
	assert.Nil(t, err)
}

func TestRepositoryAndSubdirectory(t *testing.T) {
	var plugin *Plugin
	var repo string
	var sub string
	var err error

	plugin = &Plugin{Location: "github.com/buildkite/plugins/docker-compose/beta"}
	repo, err = plugin.Repository()
	assert.Equal(t, repo, "github.com/buildkite/plugins")
	assert.Nil(t, err)
	sub, err = plugin.RepositorySubdirectory()
	assert.Equal(t, sub, "docker-compose/beta")
	assert.Nil(t, err)

	plugin = &Plugin{Location: "github.com/buildkite/test-plugin"}
	repo, err = plugin.Repository()
	assert.Equal(t, repo, "github.com/buildkite/test-plugin")
	assert.Nil(t, err)
	sub, err = plugin.RepositorySubdirectory()
	assert.Equal(t, sub, "")
	assert.Nil(t, err)

	plugin = &Plugin{Location: "github.com/buildkite"}
	repo, err = plugin.Repository()
	assert.Equal(t, repo, "")
	assert.NotNil(t, err)
	assert.Equal(t, err.Error(), `Incomplete github.com path "github.com/buildkite"`)
	sub, err = plugin.RepositorySubdirectory()
	assert.Equal(t, sub, "")
	assert.NotNil(t, err)
	assert.Equal(t, err.Error(), `Incomplete github.com path "github.com/buildkite"`)

	plugin = &Plugin{Location: "bitbucket.org/buildkite"}
	repo, err = plugin.Repository()
	assert.Equal(t, repo, "")
	assert.NotNil(t, err)
	assert.Equal(t, err.Error(), `Incomplete bitbucket.org path "bitbucket.org/buildkite"`)
	sub, err = plugin.RepositorySubdirectory()
	assert.Equal(t, sub, "")
	assert.NotNil(t, err)
	assert.Equal(t, err.Error(), `Incomplete bitbucket.org path "bitbucket.org/buildkite"`)

	plugin = &Plugin{Location: "bitbucket.org/user/project/sub/directory"}
	repo, err = plugin.Repository()
	assert.Equal(t, repo, "bitbucket.org/user/project")
	assert.Nil(t, err)
	sub, err = plugin.RepositorySubdirectory()
	assert.Equal(t, sub, "sub/directory")
	assert.Nil(t, err)

	plugin = &Plugin{Location: "114.135.234.212/foo.git"}
	repo, err = plugin.Repository()
	assert.Equal(t, repo, "114.135.234.212/foo.git")
	assert.Nil(t, err)
	sub, err = plugin.RepositorySubdirectory()
	assert.Equal(t, sub, "")
	assert.Nil(t, err)

	plugin = &Plugin{Location: "github.com/buildkite/plugins/docker-compose/beta"}
	repo, err = plugin.Repository()
	assert.Equal(t, repo, "github.com/buildkite/plugins")
	assert.Nil(t, err)
	sub, err = plugin.RepositorySubdirectory()
	assert.Equal(t, sub, "docker-compose/beta")
	assert.Nil(t, err)

	plugin = &Plugin{Location: "/Users/keithpitt/Development/plugins.git/test-plugin"}
	repo, err = plugin.Repository()
	assert.Equal(t, repo, "/Users/keithpitt/Development/plugins.git")
	assert.Nil(t, err)
	sub, err = plugin.RepositorySubdirectory()
	assert.Equal(t, sub, "test-plugin")
	assert.Nil(t, err)

	plugin = &Plugin{Location: ""}
	repo, err = plugin.Repository()
	assert.Equal(t, repo, "")
	assert.NotNil(t, err)
	assert.Equal(t, err.Error(), "Missing plugin location")
}

func TestPluginConfigurationToEnvironment(t *testing.T) {
	var env *shell.Environment
	var err error
	plugin := &Plugin{Location: "github.com/buildkite/plugins/docker-compose"}

	plugin.Configuration = map[string]interface{}{"container": "app", "some-other-setting": "else right here"}
	env, err = plugin.ConfigurationToEnvironment()
	assert.Nil(t, err)
	assert.Equal(t, env.ToSlice(), []string{"BUILDKITE_PLUGIN_DOCKER_COMPOSE_CONTAINER=app", "BUILDKITE_PLUGIN_DOCKER_COMPOSE_SOME_OTHER_SETTING=else right here"})

	plugin.Configuration = map[string]interface{}{"and _ with a    - number": 12}
	env, err = plugin.ConfigurationToEnvironment()
	assert.Nil(t, err)
	assert.Equal(t, env.ToSlice(), []string{"BUILDKITE_PLUGIN_DOCKER_COMPOSE_AND_WITH_A_NUMBER=12"})
}
