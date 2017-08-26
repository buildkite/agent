package bootstrap

import (
	"testing"

	"github.com/buildkite/agent/bootstrap/shell"
)

func TestFindingSSHTools(t *testing.T) {
	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}

	d, err := findSSHToolsDir(sh)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Found ssh tools at %s ", d)
}
