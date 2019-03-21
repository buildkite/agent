package system

import (
	"fmt"

	"github.com/denisbrodbeck/machineid"
)

var machineID string

// MachineID returns a unique string for the underlying host
func MachineID() (string, error) {
	if machineID != "" {
		return machineID, nil
	}

	// get a unique identifier for the underlying host
	var err error
	machineID, err = machineid.ProtectedID("buildkite-agent")
	if err != nil {
		return "", fmt.Errorf("Failed to find unique machine id: %v", err)
	}

	return machineID, nil
}
