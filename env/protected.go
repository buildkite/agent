package env

import "fmt"

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
// BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG is applied as `git -c <value>` before
// submodule clones (an injection vector) and has no backend customization, so
// it stays agent-authoritative rather than in checkoutOverrideScope.
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
// When updating ExecutorConfig in internal/job/config.go, ensure always-
// protected reconfigurable vars set mutableFromWithinJob here, and checkout-
// scoped vars are added to checkoutOverrideScope below.
var protectedEnv = map[string]protection{
	"BUILDKITE_AGENT_ACCESS_TOKEN":          {},
	"BUILDKITE_AGENT_DEBUG":                 {},
	"BUILDKITE_AGENT_ENDPOINT":              {},
	"BUILDKITE_AGENT_JOB_TIMEOUT_FILE":      {},
	"BUILDKITE_AGENT_PID":                   {},
	"BUILDKITE_ARTIFACT_PATHS":              {mutableFromWithinJob: true},
	"BUILDKITE_ARTIFACT_UPLOAD_DESTINATION": {mutableFromWithinJob: true},
	"BUILDKITE_BIN_PATH":                    {},
	"BUILDKITE_BUILD_PATH":                  {},
	"BUILDKITE_CHECKOUT_OVERRIDE_MODE":      {},
	"BUILDKITE_COMMAND_EVAL":                {},
	"BUILDKITE_CONFIG_PATH":                 {},
	"BUILDKITE_CONTAINER_COUNT":             {},
	"BUILDKITE_GIT_COMMIT_VERIFICATION":     {},
	"BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT":    {},
	"BUILDKITE_GIT_MIRRORS_PATH":            {},
	"BUILDKITE_GIT_MIRROR_CHECKOUT_MODE":    {},
	"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG":  {},
	"BUILDKITE_HOOKS_PATH":                  {},
	"BUILDKITE_HOOKS_SHELL":                 {},
	"BUILDKITE_KUBERNETES_EXEC":             {},
	"BUILDKITE_LOCAL_HOOKS_ENABLED":         {},
	"BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH":  {mutableFromWithinJob: true},
	"BUILDKITE_PLUGINS_ENABLED":             {},
	"BUILDKITE_PLUGINS_PATH":                {},
	"BUILDKITE_REFSPEC":                     {mutableFromWithinJob: true},
	"BUILDKITE_REPO":                        {mutableFromWithinJob: true},
	"BUILDKITE_SHELL":                       {},
	"BUILDKITE_SSH_KEYSCAN":                 {},
}

// checkoutOverrideScope contains checkout-related vars that remain mutable in
// hooks, plugins, Job API, and secrets by default so jobs can tailor checkout
// behavior. When checkout override is enabled, those same vars become locked so
// agent checkout config wins: git is riddled with shell injections, so letting a
// job set git flags would otherwise be a way to bypass protections like
// no-command-eval. Vars here must not also appear in protectedEnv; the two maps
// are disjoint.
var checkoutOverrideScope = map[string]struct{}{
	"BUILDKITE_GIT_CHECKOUT_FLAGS":              {},
	"BUILDKITE_GIT_CHECKOUT_TIMEOUT":            {},
	"BUILDKITE_GIT_CLEAN_FLAGS":                 {},
	"BUILDKITE_GIT_CLONE_FLAGS":                 {},
	"BUILDKITE_GIT_CLONE_MIRROR_FLAGS":          {},
	"BUILDKITE_GIT_FETCH_FLAGS":                 {},
	"BUILDKITE_GIT_MIRRORS_SKIP_UPDATE":         {},
	"BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS": {},
	"BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS":       {},
	"BUILDKITE_GIT_SUBMODULES":                  {},
	"BUILDKITE_SKIP_CHECKOUT":                   {},
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

// IsCheckoutOverrideScoped reports whether the environment variable is a
// checkout-related var whose write-protection depends on the checkout-override
// mode. Whether it's actually locked for a given source is decided by
// IsCheckoutLocked and IsCheckoutLockedFromWithinJob.
func IsCheckoutOverrideScoped(name string) bool {
	_, exists := checkoutOverrideScope[normalizeKeyName(name)]
	return exists
}

// CheckoutOverrideMode controls how much of the agent's checkout configuration a
// job may override. It applies only to checkoutOverrideScope vars; protectedEnv
// membership is independent of the mode.
type CheckoutOverrideMode int

const (
	// CheckoutOverrideFromJob is the default and matches the agent's historical
	// behaviour: checkout-scoped vars are locked against backend job env and
	// secrets, but hooks, plugins, and the Job API may still set them so a job
	// can tailor its own checkout.
	CheckoutOverrideFromJob CheckoutOverrideMode = iota

	// CheckoutOverrideStrict makes agent checkout config authoritative against
	// every source: backend job env, secrets, hooks, plugins, and the Job API.
	CheckoutOverrideStrict

	// CheckoutOverrideNone lets any source override agent checkout config.
	CheckoutOverrideNone
)

// Accepted flag/env values for the checkout-override modes.
const (
	checkoutOverrideFromJobName = "from-job"
	checkoutOverrideStrictName  = "strict"
	checkoutOverrideNoneName    = "none"
)

// CheckoutOverrideModeNames lists the accepted flag/env values, strictest first.
var CheckoutOverrideModeNames = []string{
	checkoutOverrideStrictName,
	checkoutOverrideFromJobName,
	checkoutOverrideNoneName,
}

func (m CheckoutOverrideMode) String() string {
	switch m {
	case CheckoutOverrideStrict:
		return checkoutOverrideStrictName
	case CheckoutOverrideNone:
		return checkoutOverrideNoneName
	default:
		return checkoutOverrideFromJobName
	}
}

// ParseCheckoutOverrideMode maps a flag/env value to a mode. An empty string
// selects the default (from-job).
func ParseCheckoutOverrideMode(s string) (CheckoutOverrideMode, error) {
	switch s {
	case "", checkoutOverrideFromJobName:
		return CheckoutOverrideFromJob, nil
	case checkoutOverrideStrictName:
		return CheckoutOverrideStrict, nil
	case checkoutOverrideNoneName:
		return CheckoutOverrideNone, nil
	default:
		return CheckoutOverrideFromJob, fmt.Errorf("invalid checkout-override mode %q, must be one of %v", s, CheckoutOverrideModeNames)
	}
}

// FlooredForCommandEval restricts the mode so command-eval can't be bypassed:
// when command-eval is disabled, CheckoutOverrideNone is raised to
// CheckoutOverrideFromJob, which still blocks backend job env and secret git
// flags (the injection vector) while leaving hooks and plugins free to tailor
// checkout.
func (m CheckoutOverrideMode) FlooredForCommandEval(commandEvalEnabled bool) CheckoutOverrideMode {
	if !commandEvalEnabled && m == CheckoutOverrideNone {
		return CheckoutOverrideFromJob
	}
	return m
}

// IsCheckoutLocked reports whether a checkout-scoped var is locked against writes
// from outside the running job (backend job env and secrets) under the given
// mode. Vars that aren't checkout-scoped are governed by IsProtected instead.
func IsCheckoutLocked(name string, mode CheckoutOverrideMode) bool {
	if !IsCheckoutOverrideScoped(name) {
		return false
	}
	return mode == CheckoutOverrideStrict || mode == CheckoutOverrideFromJob
}

// IsCheckoutLockedFromWithinJob reports whether a checkout-scoped var is locked
// against writes from within the running job (hooks, plugins, and the Job API)
// under the given mode. Vars that aren't checkout-scoped are governed by
// IsProtectedFromWithinJob instead.
func IsCheckoutLockedFromWithinJob(name string, mode CheckoutOverrideMode) bool {
	if !IsCheckoutOverrideScoped(name) {
		return false
	}
	return mode == CheckoutOverrideStrict
}
