package buildkite

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Returns the machines hostname
func MachineHostname() (string, error) {
	return run("hostname")
}

// Returns a dump of the raw operating system information
func MachineOSDump() (string, error) {
	if runtime.GOOS == "darwin" {
		return run("sw_vers")
	} else if runtime.GOOS == "linux" {
		return run("cat", "/etc/*-release")
	} else if runtime.GOOS == "windows" {
		return run("ver")
	}

	return "", nil
}

// Will return true if the machine is Windows
func MachineIsWindows() bool {
	return runtime.GOOS == "windows"
}

func run(command string, arg ...string) (string, error) {
	output, err := exec.Command(command, arg...).Output()

	if err != nil {
		Logger.Fatal(err)

		return "", err
	}

	return strings.Trim(fmt.Sprintf("%s", output), "\n"), nil
}
