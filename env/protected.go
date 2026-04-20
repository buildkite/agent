package env

type protection struct {
	// Some otherwise-protected env vars may be written from within the job
	// being executed, including hooks and plugins.
	mutableFromWithinJob bool
}

// protectedEnv contains environment variables that can only be set by agent
// configuration, or in some cases, from within the job.
//
// These variables cannot be overwritten by job-level environment variables or
// secrets, but some may still be set in hooks or plugins.
//
// For example, there is no reason for the job env provided by BK to contain
// BUILDKITE_AGENT_ACCESS_TOKEN. There's also no point for it to be modifiable
// by a plugin, hook, or the command, and even if it was modified, the agent
// wouldn't suddenly start using a different token mid-job.
//
// Similarly, BUILDKITE_COMMAND_EVAL must always come from the agent
// configuration, otherwise it would be trivial to bypass. (No-command-eval
// disables plugins, but even if it is changed by a hook, the agent doesn't
// reconfigure no-command-eval based on any changes.)
//
// Git flags are an example of vars that are primarily driven by agent config,
// but we want to allow plugins to set git flags to alter the default checkout
// process (see for example the git-clean plugin). Doing this deliberately
// reconfigures the executor (see ReadFromEnvironment and config struct tags in
// internal/job/config.go).
// But we don't want the job env from the backend to be able to set them,
// because git is riddled with shell injections, and someone with privileges to
// start a build and supply env vars could then bypass protections like
// no-command-eval.
//
// The actual enforcement of protected env within the agent level (overriding
// job-level env vars based on agent configuration) happens implicitly rather
// than relying on this map - see createEnvironment in agent/job_runner.go.
// (Once upon a time, this map _was_ used for that purpose.)
// Nowadays this map is used in a couple of other places to prevent env var
// changes, primarily to stop plugin/hook authors from shooting themselves in
// the feet (because changing the env var would have no effect, or at worst
// just break the job), and pipeline authors from doing the same with secrets.
//
// Note that this map is probably incomplete, because it was formerly used to
// filter backend-supplied vars, and such vars are still necessary for a job to
// function.
//
// When updating ExecutorConfig in internal/job/config.go, ensure
// mutableFromWithinJob is enabled here for reconfigurable vars.
var protectedEnv = map[string]protection{
	"BUILDKITE_AGENT_ACCESS_TOKEN":              {},
	"BUILDKITE_AGENT_DEBUG":                     {},
	"BUILDKITE_AGENT_ENDPOINT":                  {},
	"BUILDKITE_AGENT_PID":                       {},
	"BUILDKITE_ARTIFACT_PATHS":                  {mutableFromWithinJob: true},
	"BUILDKITE_ARTIFACT_UPLOAD_DESTINATION":     {mutableFromWithinJob: true},
	"BUILDKITE_BIN_PATH":                        {},
	"BUILDKITE_BUILD_PATH":                      {},
	"BUILDKITE_COMMAND_EVAL":                    {},
	"BUILDKITE_CONFIG_PATH":                     {},
	"BUILDKITE_CONTAINER_COUNT":                 {},
	"BUILDKITE_GIT_CHECKOUT_FLAGS":              {mutableFromWithinJob: true},
	"BUILDKITE_GIT_CLEAN_FLAGS":                 {mutableFromWithinJob: true},
	"BUILDKITE_GIT_CLONE_FLAGS":                 {mutableFromWithinJob: true},
	"BUILDKITE_GIT_CLONE_MIRROR_FLAGS":          {mutableFromWithinJob: true},
	"BUILDKITE_GIT_FETCH_FLAGS":                 {mutableFromWithinJob: true},
	"BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT":        {},
	"BUILDKITE_GIT_MIRRORS_PATH":                {},
	"BUILDKITE_GIT_MIRRORS_SKIP_UPDATE":         {mutableFromWithinJob: true},
	"BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS": {mutableFromWithinJob: true},
	"BUILDKITE_GIT_SUBMODULES":                  {mutableFromWithinJob: true},
	"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG":      {mutableFromWithinJob: true},
	"BUILDKITE_HOOKS_PATH":                      {},
	"BUILDKITE_HOOKS_SHELL":                     {},
	"BUILDKITE_KUBERNETES_EXEC":                 {},
	"BUILDKITE_LOCAL_HOOKS_ENABLED":             {},
	"BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH":      {mutableFromWithinJob: true},
	"BUILDKITE_PLUGINS_ENABLED":                 {},
	"BUILDKITE_PLUGINS_PATH":                    {},
	"BUILDKITE_REFSPEC":                         {mutableFromWithinJob: true},
	"BUILDKITE_REPO":                            {mutableFromWithinJob: true},
	"BUILDKITE_SHELL":                           {},
	"BUILDKITE_SSH_KEYSCAN":                     {},
}

// IsProtected reports whether the environment variable is write-protected when
// the write is coming from job-level env or secrets.
func IsProtected(name string) bool {
	_, exists := protectedEnv[normalizeKeyName(name)]
	return exists
}

// IsProtectedFromWithinJob reports whether the environment variable is write-
// protected when the write is coming from within the job (including hooks and
// plugins).
func IsProtectedFromWithinJob(name string) bool {
	prot, exists := protectedEnv[normalizeKeyName(name)]
	if !exists {
		return false
	}
	return !prot.mutableFromWithinJob
}
