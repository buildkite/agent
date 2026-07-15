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
// The mirror-infra vars (BUILDKITE_GIT_MIRRORS_PATH, _LOCK_TIMEOUT,
// _SKIP_UPDATE, BUILDKITE_GIT_MIRROR_CHECKOUT_MODE, and
// BUILDKITE_GIT_CLONE_MIRROR_FLAGS) are likewise agent-only: the mirror is shared
// across jobs on the host and the backend has no concept of it. CLONE_MIRROR_FLAGS
// in particular is applied to the shared `git clone --mirror`, so letting a job
// set it would be a cross-job injection vector.
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
	"BUILDKITE_GIT_CLONE_MIRROR_FLAGS":      {},
	"BUILDKITE_GIT_COMMIT_VERIFICATION":     {},
	"BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT":    {},
	"BUILDKITE_GIT_MIRRORS_PATH":            {},
	"BUILDKITE_GIT_MIRRORS_SKIP_UPDATE":     {},
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

// checkoutOverrideScope contains checkout-related vars whose write-protection
// depends on the checkout-override mode (see CheckoutOverrideMode). Under the
// default (from-job) the job may set them from pipeline/step env, hooks, plugins,
// and the Job API, overriding agent config, but secrets may not; strict locks
// them against every source; none leaves them fully open, including secrets.
// Locking matters because git is riddled with shell injections, so letting a job
// set git flags would otherwise be a way to bypass protections like
// no-command-eval (which is why disabling command-eval forces the mode to
// strict). Vars here must not also appear in protectedEnv; the two maps are
// disjoint.
var checkoutOverrideScope = map[string]struct{}{
	"BUILDKITE_GIT_CHECKOUT_FLAGS":              {},
	"BUILDKITE_GIT_CHECKOUT_TIMEOUT":            {},
	"BUILDKITE_GIT_CLEAN_FLAGS":                 {},
	"BUILDKITE_GIT_CLONE_FLAGS":                 {},
	"BUILDKITE_GIT_FETCH_FLAGS":                 {},
	"BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS": {},
	"BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS":       {},
	"BUILDKITE_GIT_SUBMODULES":                  {},
	"BUILDKITE_SKIP_CHECKOUT":                   {},
}

// Some checkout-related vars are intentionally governed by neither the mode nor
// checkoutOverrideScope. BUILDKITE_GIT_SSH_KEY and BUILDKITE_GIT_LFS_ENABLED are
// in no map at all, so any source may set them in every mode: a job supplying its
// own deploy key or LFS toggle configures its own checkout without escalating the
// agent's privileges, and neither is a shell-flag injection vector. BUILDKITE_REPO
// and BUILDKITE_REFSPEC stay mutableFromWithinJob in protectedEnv, so hooks and
// plugins may set them even under strict, while backend job env and secrets are
// still blocked by IsProtected. The checkout-override mode does not change any of
// this.

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
// IsCheckoutLocked (the job's own config sources) and IsCheckoutLockedForSecrets.
func IsCheckoutOverrideScoped(name string) bool {
	_, exists := checkoutOverrideScope[normalizeKeyName(name)]
	return exists
}

// CheckoutOverrideMode controls how much of the agent's checkout configuration a
// job may override. It applies only to checkoutOverrideScope vars; protectedEnv
// membership is independent of the mode.
type CheckoutOverrideMode int

const (
	// CheckoutOverrideFromJob is the default: the job may configure its own
	// checkout from pipeline/step env, hooks, plugins, and the Job API,
	// overriding agent config, but secrets may not set checkout-scoped vars. This
	// is deliberately more permissive than the agent's historical behaviour,
	// which locked checkout-scoped vars against backend job env.
	CheckoutOverrideFromJob CheckoutOverrideMode = iota

	// CheckoutOverrideStrict locks the checkoutOverrideScope vars against every
	// source: pipeline/step env, secrets, hooks, plugins, and the Job API. Vars
	// outside that scope (see the exclusions note on checkoutOverrideScope) are
	// unaffected by the mode.
	CheckoutOverrideStrict

	// CheckoutOverrideNone lets any source, including secrets, override the
	// checkout-override-scoped vars. Vars that are always agent-authoritative
	// (the mirror-infra vars and SUBMODULE_CLONE_CONFIG in protectedEnv) are
	// unaffected by the mode, so they stay locked even under none.
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

// RestrictedForCommandEval tightens the mode so command-eval can't be bypassed:
// when command-eval is disabled, it returns CheckoutOverrideStrict so no source
// (pipeline/step env, secrets, hooks, plugins, or the Job API) can inject git
// flags that would otherwise circumvent no-command-eval. Otherwise it returns the
// mode unchanged.
func (m CheckoutOverrideMode) RestrictedForCommandEval(commandEvalEnabled bool) CheckoutOverrideMode {
	if !commandEvalEnabled {
		return CheckoutOverrideStrict
	}
	return m
}

// IsCheckoutLocked reports whether a checkout-scoped var is locked against the
// job's own configuration sources (backend job env, hooks, plugins, and the Job
// API) under the given mode. Only strict locks these; from-job and none let the
// job configure its own checkout. Secrets are governed separately by
// IsCheckoutLockedForSecrets, and vars that aren't checkout-scoped by IsProtected
// / IsProtectedFromWithinJob.
func IsCheckoutLocked(name string, mode CheckoutOverrideMode) bool {
	if !IsCheckoutOverrideScoped(name) {
		return false
	}
	return mode == CheckoutOverrideStrict
}

// IsCheckoutLockedForSecrets reports whether a checkout-scoped var is locked
// against secret-to-env mappings under the given mode. Secrets are an external
// source, so both strict and from-job block them; only none lets a secret set
// checkout config. Vars that aren't checkout-scoped are governed by IsProtected
// instead.
func IsCheckoutLockedForSecrets(name string, mode CheckoutOverrideMode) bool {
	if !IsCheckoutOverrideScoped(name) {
		return false
	}
	return mode != CheckoutOverrideNone
}
