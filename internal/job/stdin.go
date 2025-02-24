package job

import (
	"bufio"
	"io"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/shell"
)

func readVarsFromStdin(shell *shell.Shell, stdin io.Reader) error {
	if stdin == nil {
		return nil
	}

	sc := bufio.NewScanner(stdin)
	for sc.Scan() {
		line := sc.Text()
		name, val, ok := env.Split(line)
		if !ok {
			continue
		}
		shell.Logger.Commentf("Agent process setting %s=%s", name, val)
		shell.Env.Set(name, val)
	}
	return sc.Err()
}
