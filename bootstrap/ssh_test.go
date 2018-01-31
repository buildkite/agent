package bootstrap

import (
	"testing"

	"github.com/buildkite/agent/bootstrap/shell"
)

func TestFindingSSHTools(t *testing.T) {
	t.Parallel()

	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}

	sh.Logger = shell.TestingLogger{t}

	_, err = findPathToSSHTools(sh)
	if err != nil {
		t.Fatal(err)
	}
}
