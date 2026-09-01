package process

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

func Run(l *slog.Logger, command string, arg ...string) (string, error) {
	output, err := exec.Command(command, arg...).Output()
	if err != nil {
		l.Debug(fmt.Sprintf("Could not run: %s %s (returned %s) (%T: %v)", command, arg, output, err, err))
		return "", err
	}

	return strings.Trim(string(output), "\n"), nil
}
