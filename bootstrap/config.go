package bootstrap

import (
	"reflect"

	"github.com/buildkite/agent/env"
)

// Config provides the configuration for the Bootstrap. Some of the keys are
// read from the environment after hooks are run, so we use struct tags to provide
// that mapping along with some reflection. It's a little bit magical but it's
// less work to maintain in the long run.
//
// To add a new config option that is mapped from an env, add an struct tag and it's done
type Config struct {
	// The command to run
	Command string

	// The ID of the job being run
	JobID string

	// If the bootstrap is in debug mode
	Debug bool

	// The repository that needs to be cloned
	Repository string

	// The commit being built
	Commit string

	// The branch of the commit
	Branch string

	// The tag of the job commit
	Tag string

	// Optional refspec to override git fetch
	RefSpec string `env:"BUILDKITE_REFSPEC"`

	// Plugin definition for the job
	Plugins string

	// Should git submodules be checked out
	GitSubmodules bool

	// If the commit was part of a pull request, this will container the PR number
	PullRequest string

	// The provider of the the pipeline
	PipelineProvider string

	// Slug of the current organization
	OrganizationSlug string

	// Slug of the current pipeline
	PipelineSlug string

	// Name of the agent running the bootstrap
	AgentName string

	// Should the bootstrap remove an existing checkout before running the job
	CleanCheckout bool

	// Flags to pass to "git clone" command
	GitCloneFlags string `env:"BUILDKITE_GIT_CLONE_FLAGS"`

	// Flags to pass to "git clean" command
	GitCleanFlags string `env:"BUILDKITE_GIT_CLEAN_FLAGS"`

	// Whether or not to run the hooks/commands in a PTY
	RunInPty bool

	// Are aribtary commands allowed to be executed
	CommandEval bool

	// Are plugins enabled?
	PluginsEnabled bool

	// Whether to validate plugin configuration
	PluginValidation bool

	// Are local hooks enabled?
	LocalHooksEnabled bool

	// Path where the builds will be run
	BuildPath string

	// Path to the buildkite-agent binary
	BinPath string

	// Path to the global hooks
	HooksPath string

	// Path to the plugins directory
	PluginsPath string

	// Paths to automatically upload as artifacts when the build finishes
	AutomaticArtifactUploadPaths string `env:"BUILDKITE_ARTIFACT_PATHS"`

	// A custom destination to upload artifacts to (i.e. s3://...)
	ArtifactUploadDestination string `env:"BUILDKITE_ARTIFACT_UPLOAD_DESTINATION"`

	// Whether ssh-keyscan is run on ssh hosts before checkout
	SSHKeyscan bool

	// The shell used to execute commands
	Shell string
}

// ReadFromEnvironment reads configuration from the Environment, returns a map
// of the env keys that changed and the new values
func (c *Config) ReadFromEnvironment(environ *env.Environment) map[string]string {
	changed := map[string]string{}

	// Use reflection for the type and values
	t := reflect.TypeOf(*c)
	v := reflect.ValueOf(c).Elem()

	// Iterate over all available fields and read the tag value
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Find struct fields with env tag
		if tag := field.Tag.Get("env"); tag != "" && environ.Exists(tag) {
			newValue, _ := environ.Get(tag)

			// We only care if the value has changed
			if newValue != value.String() {
				value.SetString(newValue)
				changed[tag] = newValue
			}
		}
	}

	return changed
}
