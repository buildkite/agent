package buildbox

import (
	"fmt"
	"os/exec"
	"strings"
)

func MachineHostname() string {
	// Figure out the hostname of the current machine
	hostname, err := exec.Command("hostname").Output()
	if err != nil {
		Logger.Fatal(err)
	}

	// Retrun a trimmed hostname
	return strings.Trim(fmt.Sprintf("%s", hostname), "\n")
}
