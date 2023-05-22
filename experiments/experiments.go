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

const (
	PolyglotHooks              = "polyglot-hooks"
	JobAPI                     = "job-api"
	KubernetesExec             = "kubernetes-exec"
	ANSITimestamps             = "ansi-timestamps"
	GitMirrors                 = "git-mirrors"
	FlockFileLocks             = "flock-file-locks"
	ResolveCommitAfterCheckout = "resolve-commit-after-checkout"
	DescendingSpawnPrioity     = "descending-spawn-priority"
	InbuiltStatusPage          = "inbuilt-status-page"
	AgentAPI                   = "agent-api"
	NormalisedUploadPaths      = "normalised-upload-paths"

	StateKnown    State = "known"
	StatePromoted State = "promoted"
	StateUnknown  State = "unknown"
)

var (
	Available = map[string]struct{}{
		PolyglotHooks:              {},
		JobAPI:                     {},
		KubernetesExec:             {},
		ANSITimestamps:             {},
		FlockFileLocks:             {},
		ResolveCommitAfterCheckout: {},
		DescendingSpawnPrioity:     {},
		InbuiltStatusPage:          {},
		AgentAPI:                   {},
		NormalisedUploadPaths:      {},
	}

	Promoted = map[string]string{
		GitMirrors: fmt.Sprintf("The %s experiment has been promoted to a stable feature. You can safely remove the `--experiment %s` flag to silence this message and continue using the feature", GitMirrors, GitMirrors),
	}

	experiments = make(map[string]bool, len(Available))
)

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
