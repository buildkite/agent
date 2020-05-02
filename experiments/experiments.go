package experiments

var experiments = make(map[string]bool)

// Enable a particular experiment in the agent
func Enable(experiment string) {
	experiments[experiment] = true
}

// Disable a particular experiment in the agent
func Disable(experiment string) {
	delete(experiments, experiment)
}

// Check if an experiment has been enabled
func IsEnabled(experiment string) bool {
	if val, ok := experiments[experiment]; ok {
		return val
	} else {
		return false
	}
}

// Enabled returns the keys of all the enabled experiments
func Enabled() []string {
	var enabled []string
	for exp, ok := range experiments {
		if ok {
			enabled = append(enabled, exp)
		}
	}
	return enabled
}
