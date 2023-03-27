// Package experiments provides a global registry of enabled and disabled
// experiments.
//
// It is intended for internal use by buildkite-agent only.
package experiments

var (
	Available = map[string]struct{}{
		"job-api":                       {},
		"kubernetes-exec":               {},
		"ansi-timestamps":               {},
		"git-mirrors":                   {},
		"flock-file-locks":              {},
		"resolve-commit-after-checkout": {},
		"descending-spawn-priority":     {},
		"inbuilt-status-page":           {},
	}

	experiments = make(map[string]bool, len(Available))
)

// Enable a particular experiment in the agent.
func Enable(key string) (known bool) {
	experiments[key] = true
	_, known = Available[key] // is the experiment they've enabled one that we know of?
	return known
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
