// Package experiments provides a global registry of enabled and disabled
// experiments.
//
// It is intended for internal use by buildkite-agent only.
package experiments

import (
	"context"
	"fmt"
	"sync"

	"github.com/buildkite/agent/v4/logger"
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
	DescendingSpawnPriority        = "descending-spawn-priority"
	InterpolationPrefersRuntimeEnv = "interpolation-prefers-runtime-env"
	PTYRaw                         = "pty-raw"
	ZipPlugins                     = "zip-plugins"

	// Promoted or removed experiments - un-export these to ensure no new code
	// can depend on them.
	allowArtifactPathTraversal = "allow-artifact-path-traversal"
	ansiTimestamps             = "ansi-timestamps"
	avoidRecursiveTrap         = "avoid-recursive-trap"
	flockFileLocks             = "flock-file-locks"
	gitMirrors                 = "git-mirrors"
	inbuiltStatusPage          = "inbuilt-status-page"
	isolatedPluginCheckout     = "isolated-plugin-checkout"
	jobAPI                     = "job-api"
	kubernetesExec             = "kubernetes-exec"
	normalisedUploadPaths      = "normalised-upload-paths"
	overrideZeroExitOnCancel   = "override-zero-exit-on-cancel"
	polyglotHooks              = "polyglot-hooks"
	propagateAgentConfigVars   = "propagate-agent-config-vars"
	resolveCommitAfterCheckout = "resolve-commit-after-checkout"
	useZZGlob                  = "use-zzglob"
)

var (
	Available = map[string]struct{}{
		AgentAPI:                       {},
		DescendingSpawnPriority:        {},
		InterpolationPrefersRuntimeEnv: {},
		PTYRaw:                         {},
		ZipPlugins:                     {},
	}

	Promoted = map[string]string{
		ansiTimestamps:             standardPromotionMsg(ansiTimestamps, "v3.48.0"),
		allowArtifactPathTraversal: "The allow-artifact-path-traversal escape-hatch experiment has been removed as of agent v4, because the path traversal behaviour was insecure",
		avoidRecursiveTrap:         standardPromotionMsg(avoidRecursiveTrap, "v3.66.0"),
		flockFileLocks:             standardPromotionMsg(flockFileLocks, "v3.48.0"),
		gitMirrors:                 standardPromotionMsg(gitMirrors, "v3.47.0"),
		inbuiltStatusPage:          standardPromotionMsg(inbuiltStatusPage, "v3.48.0"),
		isolatedPluginCheckout:     standardPromotionMsg(isolatedPluginCheckout, "v3.67.0"),
		jobAPI:                     standardPromotionMsg(jobAPI, "v3.64.0"),
		kubernetesExec:             "The kubernetes-exec experiment has been replaced with the --kubernetes-exec flag as of agent v3.74.0",
		normalisedUploadPaths:      standardPromotionMsg(normalisedUploadPaths, "v4"),
		overrideZeroExitOnCancel:   standardPromotionMsg(overrideZeroExitOnCancel, "v4"),
		polyglotHooks:              standardPromotionMsg(polyglotHooks, "v3.85.0"),
		propagateAgentConfigVars:   standardPromotionMsg(propagateAgentConfigVars, "v4"),
		resolveCommitAfterCheckout: standardPromotionMsg(resolveCommitAfterCheckout, "v4"),
		useZZGlob:                  standardPromotionMsg(useZZGlob, "v3.104.0"),
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
		l.Warnf("Unknown experiment %q", key)
	case StatePromoted:
		l.Warnf(Promoted[key])
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
