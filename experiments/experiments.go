package experiments

var experiments = make(map[string]bool)

// Enable a particular experiment in the agent
func Enable(key string) {
	experiments[key] = true
}

// Disable a particular experiment in the agent
func Disable(key string) {
	delete(experiments, key)
}

// IsEnabled returns whether the named experiment is enabled
func IsEnabled(key string) bool {
	return experiments[key] // map[T]bool returns false for missing keys
}

// Enabled returns the keys of all the enabled experiments
func Enabled() []string {
	var keys []string
	for key, enabled := range experiments {
		if enabled {
			keys = append(keys, key)
		}
	}
	return keys
}
