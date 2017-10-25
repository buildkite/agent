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

	sh.Debug = true
	sh.Logger = shell.TestingLogger{t}

	d, err := findPathToSSHTools(sh)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Found ssh tools at %s ", d)
}
