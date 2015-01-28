package buildkite

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Returns the machines hostname
func MachineHostname() string {
	if hostname, err := run("hostname"); err != nil {
		return ""
	} else {
		return hostname
	}
}

// Returns operating system information about the current machine
func MachineOS() string {
	command := ""

	if runtime.GOOS == "darwin" {
		command = "sw_vers"
	} else if runtime.GOOS == "linux" {
		command = "cat /etc/*-release"
	} else if runtime.GOOS == "windows" {
		command = "ver"
	}

	if command != "" {
		if hostname, err := run(command); err != nil {
			return ""
		} else {
			return hostname
		}
	} else {
		return ""
	}
}

// Will return true if the machine is Windows
func MachineIsWindows() bool {
	return runtime.GOOS == "windows"
}

func run(command string) (string, error) {
	output, err := exec.Command(command).Output()

	if err != nil {
		Logger.Fatal(err)

		return "", err
	}

	return strings.Trim(fmt.Sprintf("%s", output), "\n"), nil
}
