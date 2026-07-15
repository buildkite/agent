package job

import (
	"log"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/process"
	"github.com/buildkite/agent/v3/tracetools"
)

// Config provides the configuration for the job executor. Some of the keys are
// read from the environment after hooks are run, so we use struct tags to provide
// that mapping along with some reflection. It's a little bit magical but it's
// less work to maintain in the long run.
//
// To add a new config option that is mapped from an environment variable, add a
// struct tag, then don't forget to add a corresponding CLI flag over in the
// clicommand/bootstrap.go(BootstrapConfig) struct, otherwise it won't work.
// Also check protectedEnv and checkoutOverrideScope in env/protected.go.

type ExecutorConfig struct {
	// The command to run
	Command string

	// The ID of the job being run
	JobID string

	// If the executor is in debug mode
	Debug bool

	// The repository that needs to be cloned
	Repository string `env:"BUILDKITE_REPO"`

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
	GitSubmodules bool `env:"BUILDKITE_GIT_SUBMODULES"`

	// Whether to enable Git LFS operations during checkout
	GitLFSEnabled bool `env:"BUILDKITE_GIT_LFS_ENABLED"`

	// If the commit was part of a pull request, this will container the PR number
	PullRequest string

	// Whether the agent should attempt to checkout the pull request commit using the merge refspec
	PullRequestUsingMergeRefspec bool

	// The provider of the pipeline
	PipelineProvider string

	// Slug of the current organization
	OrganizationSlug string

	// Slug of the current pipeline
	PipelineSlug string

	// Name of the agent running the job
	AgentName string

	// Name of the queue the agent belongs to, if tagged
	Queue string

	// Should the executor remove an existing checkout before running the job
	CleanCheckout bool `env:"BUILDKITE_CLEAN_CHECKOUT"`

	// Skip the checkout phase entirely
	SkipCheckout bool `env:"BUILDKITE_SKIP_CHECKOUT"`

	// Comma-separated list of paths for git sparse checkout (cone mode).
	GitSparseCheckoutPaths []string `env:"BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS"`

	// Skip git fetch if the commit already exists locally
	GitSkipFetchExistingCommits bool `env:"BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS"`

	// Controls which sources may override the agent's checkout settings.
	// Intentionally has no env tag so hooks cannot relax it at runtime.
	CheckoutOverrideMode env.CheckoutOverrideMode

	// Timeout in seconds for the git checkout phase (0 means no timeout)
	GitCheckoutTimeout int `env:"BUILDKITE_GIT_CHECKOUT_TIMEOUT"`

	// Flags to pass to "git checkout" command
	GitCheckoutFlags string `env:"BUILDKITE_GIT_CHECKOUT_FLAGS"`

	// Flags to pass to "git clone" command
	GitCloneFlags string `env:"BUILDKITE_GIT_CLONE_FLAGS"`

	// Flags to pass to "git fetch" command
	GitFetchFlags string `env:"BUILDKITE_GIT_FETCH_FLAGS"`

	// Flags to pass to "git clone" command for mirroring
	GitCloneMirrorFlags string `env:"BUILDKITE_GIT_CLONE_MIRROR_FLAGS"`

	// Selects among preconfigured sets of flags for clones from a mirror
	GitMirrorCheckoutMode string `env:"BUILDKITE_GIT_MIRROR_CHECKOUT_MODE"`

	// Flags to pass to "git clean" command
	GitCleanFlags string `env:"BUILDKITE_GIT_CLEAN_FLAGS"`

	// SSH private key to use for git checkout operations
	GitSSHKey string `env:"BUILDKITE_GIT_SSH_KEY"`

	// Enable git commit verification
	GitCommitVerification string

	// Config key=value pairs to pass to "git" when submodule init commands are invoked
	GitSubmoduleCloneConfig []string `env:"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG"`

	// Whether or not to run the hooks/commands in a PTY
	RunInPty bool

	// Are arbitrary commands allowed to be executed
	CommandEval bool

	// Are plugins enabled?
	PluginsEnabled bool

	// Should we always force a fresh clone of plugins, even if we have a local checkout?
	PluginsAlwaysCloneFresh bool `env:"BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH"`

	// Whether to validate plugin configuration
	PluginValidation bool

	// Are local hooks enabled?
	LocalHooksEnabled bool

	// Should we enforce that only one checkout and one command hook are run?
	StrictSingleHooks bool

	// Path where the builds will be run
	BuildPath string

	// Path where the sockets are stored
	SocketsPath string

	// Path where the repository mirrors are stored
	GitMirrorsPath string

	// Seconds to wait before allowing git mirror clone lock to be acquired
	GitMirrorsLockTimeout int

	// Skip updating the Git mirror before using it
	GitMirrorsSkipUpdate bool `env:"BUILDKITE_GIT_MIRRORS_SKIP_UPDATE"`

	// Path to the buildkite-agent binary
	BinPath string

	// Path to the global hooks
	HooksPath string

	// Additional hooks directories that can be provided
	AdditionalHooksPaths []string

	// Path to the plugins directory
	PluginsPath string

	// Paths to automatically upload as artifacts when the build finishes
	AutomaticArtifactUploadPaths string `env:"BUILDKITE_ARTIFACT_PATHS"`

	// A custom destination to upload artifacts to (for example, s3://...)
	ArtifactUploadDestination string `env:"BUILDKITE_ARTIFACT_UPLOAD_DESTINATION"`

	// Whether ssh-keyscan is run on ssh hosts before checkout
	SSHKeyscan bool

	// The shell used to execute commands
	Shell string

	// The shell used to execute agent hooks
	HooksShell string

	// Phases to execute, defaults to all phases
	Phases []string

	// What signal to use for command cancellation
	CancelSignal process.Signal

	// Amount of time to wait between sending the CancelSignal and SIGKILL to the process groups
	// that the executor starts. The subprocesses should use this time to clean up after themselves.
	SignalGracePeriod time.Duration

	// List of environment variable globs to redact from job output
	RedactedVars []string

	// Backend to use for tracing. If an empty string, no tracing will occur.
	TracingBackend string

	// Service name to use when reporting traces.
	TracingServiceName string

	// Traceing context information
	TracingTraceParent string

	// W3C tracestate accompanying TracingTraceParent. Plumbed through to the
	// bootstrap environment whenever the server provides a value, but only
	// attached to the OTel span context when TracingPropagateTraceparent is
	// enabled (same opt-in gate as TracingTraceParent).
	TracingTraceState string

	// Accept traceparent context from Buildkite control plane
	TracingPropagateTraceparent bool

	// Encoding (within base64) for the trace context environment variable.
	TraceContextCodec tracetools.Codec

	// Whether to start the JobAPI
	JobAPI bool

	// The warnings that have been disabled by the user
	DisabledWarnings []string

	// Secrets definition for the job step
	Secrets string

	// Number of checkout attempts (including the initial attempt).
	// Uses exponential backoff with jitter between retries.
	CheckoutAttempts int
}

// ReadFromEnvironment reads configuration from the Environment, returns a map
// of the env keys that changed and the new values
func (c *ExecutorConfig) ReadFromEnvironment(environ *env.Environment) map[string]string {
	changed := map[string]string{}

	// Use reflection for the type and values
	fields := reflect.TypeFor[ExecutorConfig]()
	values := reflect.ValueOf(c).Elem()

	// Iterate over all available fields and read the tag value
	for i := range fields.NumField() {
		f := fields.Field(i)
		v := values.Field(i)

		// Find struct fields with env tag
		if tag := f.Tag.Get("env"); tag != "" && environ.Exists(tag) {
			// ReadFromEnvironment runs after applyEnvironmentChanges, so the
			// checkout vars here come from within the job (hooks/plugins). from-job
			// and none let them reconfigure checkout; only strict locks them.
			if env.IsCheckoutLocked(tag, c.CheckoutOverrideMode) {
				continue
			}

			newStr, _ := environ.Get(tag)

			switch v.Kind() {
			case reflect.String:
				if newStr == v.String() {
					break
				}
				v.SetString(newStr)
				changed[tag] = newStr

			case reflect.Bool:
				newBool, err := strconv.ParseBool(newStr)
				if err != nil {
					// Don't log the value: this env may hold secret-backed values
					// (see setUp in executor.go) and this logger bypasses redaction.
					log.Printf("warning: cannot parse %s as bool, ignoring", tag)
					break
				}
				if newBool == v.Bool() {
					break
				}
				v.SetBool(newBool)
				changed[tag] = newStr

			case reflect.Int:
				newInt, err := strconv.Atoi(newStr)
				if err != nil {
					log.Printf("warning: cannot parse %s as int, ignoring", tag)
					break
				}
				if int64(newInt) == v.Int() {
					break
				}
				v.SetInt(int64(newInt))
				changed[tag] = newStr

			case reflect.Slice:
				if v.Type().Elem() != reflect.TypeFor[string]() {
					log.Printf("warning: cannot parse %s as %v, ignoring", tag, v.Type())
					break
				}
				var newSlice []string
				if newStr != "" {
					newSlice = strings.Split(newStr, ",")
				}
				if slices.Equal(newSlice, v.Interface().([]string)) {
					break
				}
				v.Set(reflect.ValueOf(newSlice))
				changed[tag] = newStr

			default:
				log.Printf("warning: job.ExecutorConfig.ReadFromEnvironment does not support %v for %s", v.Kind(), tag)
			}
		}
	}

	return changed
}
