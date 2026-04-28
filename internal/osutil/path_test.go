package osutil

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func TestNormalizingHomeDirectories(t *testing.T) {
	t.Parallel()

	usr, err := user.Current()
	if err != nil {
		t.Errorf("err error = %v, want nil", err)
	}

	fp, err := NormalizeFilePath(filepath.Join("~", ".ssh"))
	if err != nil {
		t.Errorf("err error = %v, want nil", err)
	}
	if got, want := filepath.Join(usr.HomeDir, ".ssh"), fp; got != want {
		t.Errorf("filepath.Join(usr.HomeDir, \".ssh\") = %q, want %q", got, want)
	}
	if got := filepath.IsAbs(fp); !got {
		t.Errorf("filepath.IsAbs(fp) = %t, want true", got)
	}
}

func TestNormalizingFilePaths(t *testing.T) {
	t.Parallel()

	workingDir, err := os.Getwd()
	if err != nil {
		t.Errorf("err error = %v, want nil", err)
	}

	fp, err := NormalizeFilePath(filepath.Join(".", "builds"))
	if err != nil {
		t.Errorf("err error = %v, want nil", err)
	}
	if got, want := filepath.Join(workingDir, "builds"), fp; got != want {
		t.Errorf("filepath.Join(workingDir, \"builds\") = %q, want %q", got, want)
	}
	if got := filepath.IsAbs(fp); !got {
		t.Errorf("filepath.IsAbs(fp) = %t, want true", got)
	}
}

func TestNormalizingEmptyPaths(t *testing.T) {
	t.Parallel()

	fp, err := NormalizeFilePath("")
	if err != nil {
		t.Errorf("err error = %v, want nil", err)
	}
	if got, want := fp, ""; got != want {
		t.Errorf("fp = %q, want %q", got, want)
	}
}

func TestNormalizingCommands(t *testing.T) {
	t.Parallel()

	usr, err := user.Current()
	if err != nil {
		t.Errorf("err error = %v, want nil", err)
	}

	c, err := NormalizeCommand(filepath.Join("~/", "buildkite-agent", "bootstrap.sh"))
	if err != nil {
		t.Errorf("err error = %v, want nil", err)
	}
	if got, want := filepath.Join(usr.HomeDir, "buildkite-agent", "bootstrap.sh"), c; got != want {
		t.Errorf("filepath.Join(usr.HomeDir, \"buildkite-agent\", \"bootstrap.sh\") = %q, want %q", got, want)
	}

	c, err = NormalizeCommand("cat test.log")
	if err != nil {
		t.Errorf("err error = %v, want nil", err)
	}
	if got, want := c, "cat test.log"; got != want {
		t.Errorf("c = %q, want %q", got, want)
	}
}
