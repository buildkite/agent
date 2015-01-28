package buildkite

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Returns the machines hostname
func MachineHostname() (string, error) {
	hostname, err := run("hostname")

	if err != nil {
		Logger.Fatalf("Could not retrieve hostname: %s", hostname)
	}

	return hostname, err
}

// Returns a dump of the raw operating system information
func MachineOSDump() (string, error) {
	if runtime.GOOS == "darwin" {
		return run("sw_vers")
	} else if runtime.GOOS == "linux" {
		return cat("/etc/*-release"), nil
	} else if runtime.GOOS == "windows" {
		return run("ver")
	}

	return "", nil
}

// Will return true if the machine is Windows
func MachineIsWindows() bool {
	return runtime.GOOS == "windows"
}

// Replicates how the command line tool `cat` works, but is more verbose about
// what it does
func cat(pathToRead string) string {
	files, err := filepath.Glob(pathToRead)

	if err != nil {
		Logger.Debugf("Failed to get list of files for OS Dump: %s", pathToRead)

		return ""
	} else {
		var buffer bytes.Buffer

		for _, file := range files {
			data, err := ioutil.ReadFile(file)

			if err != nil {
				Logger.Debugf("Could not read file for OS Dump: %s (%T: %v)", file, err, err)
			} else {
				buffer.WriteString(string(data))
			}
		}

		return buffer.String()
	}
}

func run(command string, arg ...string) (string, error) {
	output, err := exec.Command(command, arg...).Output()

	if err != nil {
		Logger.Debugf("Could not run: %s %s (returned %s) (%T: %v)", command, arg, output, err, err)
		return "", err
	}

	return strings.Trim(fmt.Sprintf("%s", output), "\n"), nil
}
