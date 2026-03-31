package process

import (
	"os/exec"
	"strings"

	"github.com/buildkite/agent/v4/logger"
)

func Run(l logger.Logger, command string, arg ...string) (string, error) {
	output, err := exec.Command(command, arg...).Output()
	if err != nil {
		l.Debugf("Could not run: %s %s (returned %s) (%T: %v)", command, arg, output, err, err)
		return "", err
	}

	return strings.Trim(string(output), "\n"), nil
}
