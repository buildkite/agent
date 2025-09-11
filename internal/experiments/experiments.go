// Package experiments provides a global registry of enabled and disabled
// experiments.
//
// It is intended for internal use by buildkite-agent only.
package experiments

import (
	"context"
	"fmt"
	"sync"

	"github.com/buildkite/agent/v3/logger"
)

type State string

// Experiment states
const (
	StateKnown    State = "known"
	StatePromoted State = "promoted"
	StateUnknown  State = "unknown"
)

const (
	// Available experiments
	AgentAPI                       = "agent-api"
	AllowArtifactPathTraversal     = "allow-artifact-path-traversal"
	DescendingSpawnPriority        = "descending-spawn-priority"
	InterpolationPrefersRuntimeEnv = "interpolation-prefers-runtime-env"
	NormalisedUploadPaths          = "normalised-upload-paths"
	OverrideZeroExitOnCancel       = "override-zero-exit-on-cancel"
	PTYRaw                         = "pty-raw"
	ResolveCommitAfterCheckout     = "resolve-commit-after-checkout"
	PropagateAgentConfigVars       = "propagate-agent-config-vars"

	// Promoted experiments
	ANSITimestamps         = "ansi-timestamps"
	AvoidRecursiveTrap     = "avoid-recursive-trap"
	FlockFileLocks         = "flock-file-locks"
	GitMirrors             = "git-mirrors"
	InbuiltStatusPage      = "inbuilt-status-page"
	IsolatedPluginCheckout = "isolated-plugin-checkout"
	JobAPI                 = "job-api"
	KubernetesExec         = "kubernetes-exec"
	PolyglotHooks          = "polyglot-hooks"
	UseZZGlob              = "use-zzglob"
)

var (
	Available = map[string]struct{}{
		AgentAPI:                       {},
		AllowArtifactPathTraversal:     {},
		DescendingSpawnPriority:        {},
		InterpolationPrefersRuntimeEnv: {},
		NormalisedUploadPaths:          {},
		OverrideZeroExitOnCancel:       {},
		PTYRaw:                         {},
		ResolveCommitAfterCheckout:     {},
		PropagateAgentConfigVars:       {},
	}

	Promoted = map[string]string{
		ANSITimestamps:         standardPromotionMsg(ANSITimestamps, "v3.48.0"),
		AvoidRecursiveTrap:     standardPromotionMsg(AvoidRecursiveTrap, "v3.66.0"),
		FlockFileLocks:         standardPromotionMsg(FlockFileLocks, "v3.48.0"),
		GitMirrors:             standardPromotionMsg(GitMirrors, "v3.47.0"),
		InbuiltStatusPage:      standardPromotionMsg(InbuiltStatusPage, "v3.48.0"),
		IsolatedPluginCheckout: standardPromotionMsg(IsolatedPluginCheckout, "v3.67.0"),
		JobAPI:                 standardPromotionMsg(JobAPI, "v3.64.0"),
		KubernetesExec:         "The kubernetes-exec experiment has been replaced with the --kubernetes-exec flag as of agent v3.74.0",
		PolyglotHooks:          standardPromotionMsg(PolyglotHooks, "v3.85.0"),
		UseZZGlob:              standardPromotionMsg(UseZZGlob, "v3.104.0"),
	}

	// Used to track experiments possibly in use.
	allMu sync.Mutex
	all   = make(map[string]struct{})
)

func standardPromotionMsg(key, version string) string {
	return fmt.Sprintf("The %s experiment has been promoted to a stable feature in agent version %s. You can safely remove the `--experiment %s` flag to silence this message and continue using the feature", key, version, key)
}

type experimentCtxKey struct {
	experiment string
}

// EnableWithWarnings enables an experiment in a new context, logging
// information about unknown and promoted experiments.
func EnableWithWarnings(ctx context.Context, l logger.Logger, key string) (context.Context, State) {
	newctx, state := Enable(ctx, key)
	switch state {
	case StateKnown:
	// Noop
	case StateUnknown:
		l.Warn("Unknown experiment %q", key)
	case StatePromoted:
		l.Warn(Promoted[key])
	}
	return newctx, state
}

// Enable a particular experiment in a new context.
func Enable(ctx context.Context, key string) (newctx context.Context, state State) {
	allMu.Lock()
	all[key] = struct{}{}
	allMu.Unlock()

	newctx = context.WithValue(ctx, experimentCtxKey{key}, true)

	if _, promoted := Promoted[key]; promoted {
		return newctx, StatePromoted
	}

	if _, known := Available[key]; known {
		return newctx, StateKnown
	}

	return newctx, StateUnknown
}

// Disable a particular experiment in a new context.
func Disable(ctx context.Context, key string) context.Context {
	// Even if we learn about the experiment through disablement, it is still
	// an experiment...
	allMu.Lock()
	all[key] = struct{}{}
	allMu.Unlock()

	return context.WithValue(ctx, experimentCtxKey{key}, false)
}

// IsEnabled reports whether the named experiment is enabled in the context.
func IsEnabled(ctx context.Context, key string) bool {
	state := ctx.Value(experimentCtxKey{key})
	return state != nil && state.(bool)
}

// KnownAndEnabled returns the keys of all the known and enabled experiments.
func KnownAndEnabled(ctx context.Context) []string {
	allMu.Lock()
	defer allMu.Unlock()
	var keys []string
	for key := range all {
		if _, known := Available[key]; known && IsEnabled(ctx, key) {
			keys = append(keys, key)
		}
	}
	return keys
}

// Enabled returns the keys of all the enabled experiments.
func Enabled(ctx context.Context) []string {
	allMu.Lock()
	defer allMu.Unlock()
	var keys []string
	for key := range all {
		if IsEnabled(ctx, key) {
			keys = append(keys, key)
		}
	}
	return keys
}
