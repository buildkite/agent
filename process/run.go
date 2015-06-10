package process

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/buildkite/agent/logger"
)

func Run(command string, arg ...string) (string, error) {
	output, err := exec.Command(command, arg...).Output()

	if err != nil {
		logger.Debug("Could not run: %s %s (returned %s) (%T: %v)", command, arg, output, err, err)
		return "", err
	}

	return strings.Trim(fmt.Sprintf("%s", output), "\n"), nil
}
