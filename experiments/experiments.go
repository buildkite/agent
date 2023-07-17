// Package experiments provides a global registry of enabled and disabled
// experiments.
//
// It is intended for internal use by buildkite-agent only.
package experiments

import (
	"fmt"

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
	AgentAPI                   = "agent-api"
	DescendingSpawnPrioity     = "descending-spawn-priority"
	JobAPI                     = "job-api"
	KubernetesExec             = "kubernetes-exec"
	NormalisedUploadPaths      = "normalised-upload-paths"
	PolyglotHooks              = "polyglot-hooks"
	ResolveCommitAfterCheckout = "resolve-commit-after-checkout"
	AvoidRecursiveTrap         = "avoid-recursive-trap"

	// Promoted experiments
	ANSITimestamps    = "ansi-timestamps"
	FlockFileLocks    = "flock-file-locks"
	GitMirrors        = "git-mirrors"
	InbuiltStatusPage = "inbuilt-status-page"
)

var (
	Available = map[string]struct{}{
		AgentAPI:                   {},
		DescendingSpawnPrioity:     {},
		JobAPI:                     {},
		KubernetesExec:             {},
		NormalisedUploadPaths:      {},
		PolyglotHooks:              {},
		ResolveCommitAfterCheckout: {},
	}

	Promoted = map[string]string{
		ANSITimestamps:    standardPromotionMsg(ANSITimestamps, "v3.48.0"),
		FlockFileLocks:    standardPromotionMsg(FlockFileLocks, "v3.48.0"),
		GitMirrors:        standardPromotionMsg(GitMirrors, "v3.47.0"),
		InbuiltStatusPage: standardPromotionMsg(InbuiltStatusPage, "v3.48.0"),
	}

	experiments = make(map[string]bool, len(Available))
)

func standardPromotionMsg(key, version string) string {
	return fmt.Sprintf("The %s experiment has been promoted to a stable feature in agent version %s. You can safely remove the `--experiment %s` flag to silence this message and continue using the feature", key, version, key)
}

func EnableWithUndo(key string) func() {
	Enable(key)
	return func() { Disable(key) }
}

func EnableWithWarnings(l logger.Logger, key string) State {
	state := Enable(key)
	switch state {
	case StateKnown:
	// Noop
	case StateUnknown:
		l.Warn("Unknown experiment %q", key)
	case StatePromoted:
		l.Warn(Promoted[key])
	}
	return state
}

// Enable a particular experiment in the agent.
func Enable(key string) (state State) {
	experiments[key] = true

	if _, promoted := Promoted[key]; promoted {
		return StatePromoted
	}

	if _, known := Available[key]; known {
		return StateKnown
	}

	return StateUnknown
}

// Disable a particular experiment in the agent.
func Disable(key string) {
	delete(experiments, key)
}

// IsEnabled reports whether the named experiment is enabled.
func IsEnabled(key string) bool {
	return experiments[key] // map[T]bool returns false for missing keys
}

// Enabled returns the keys of all the enabled experiments.
func Enabled() []string {
	var keys []string
	for key, enabled := range experiments {
		if enabled {
			keys = append(keys, key)
		}
	}
	return keys
}
