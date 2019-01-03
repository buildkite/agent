package experiments

var experiments = make(map[string]bool)

// Enable a paticular experiment in the agent
func Enable(experiment string) {
	experiments[experiment] = true
}

// Check if an experiment has been enabled
func IsEnabled(experiment string) bool {
	if val, ok := experiments[experiment]; ok {
		return val
	} else {
		return false
	}
}
