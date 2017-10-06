package bootstrap

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/bootstrap/shell"
)

func TestCheckingIfAHostExists(t *testing.T) {
	t.Parallel()

	dir, err := ioutil.TempDir("", "known-hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}

	sh.Debug = true
	sh.Logger = shell.TestingLogger{t}

	kh := knownHosts{Shell: sh, Path: filepath.Join(dir, "known_hosts")}

	exists, err := kh.Contains("github.com")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("Host shouldn't exist yet")
	}

	if err := kh.Add("github.com"); err != nil {
		t.Fatal(err)
	}

	exists, err = kh.Contains("github.com")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("Host should exist")
	}
}
