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
		t.Errorf("user.Current() error = %v, want nil", err)
	}

	fp, err := NormalizeFilePath(filepath.Join("~", ".ssh"))
	if err != nil {
		t.Errorf("NormalizeFilePath(%q) error = %v, want nil", filepath.Join("~", ".ssh"), err)
	}
	if got, want := fp, filepath.Join(usr.HomeDir, ".ssh"); got != want {
		t.Errorf("NormalizeFilePath(%q) = %q, want %q", filepath.Join("~", ".ssh"), got, want)
	}
	if got := filepath.IsAbs(fp); !got {
		t.Errorf("filepath.IsAbs(fp) = %t, want true", got)
	}
}

func TestNormalizingFilePaths(t *testing.T) {
	t.Parallel()

	workingDir, err := os.Getwd()
	if err != nil {
		t.Errorf("os.Getwd() error = %v, want nil", err)
	}

	fp, err := NormalizeFilePath(filepath.Join(".", "builds"))
	if err != nil {
		t.Errorf("NormalizeFilePath(%q) error = %v, want nil", filepath.Join(".", "builds"), err)
	}
	if got, want := fp, filepath.Join(workingDir, "builds"); got != want {
		t.Errorf("NormalizeFilePath(%q) = %q, want %q", filepath.Join(".", "builds"), got, want)
	}
	if got := filepath.IsAbs(fp); !got {
		t.Errorf("filepath.IsAbs(fp) = %t, want true", got)
	}
}

func TestNormalizingEmptyPaths(t *testing.T) {
	t.Parallel()

	fp, err := NormalizeFilePath("")
	if err != nil {
		t.Errorf("NormalizeFilePath(%q) error = %v, want nil", "", err)
	}
	if got, want := fp, ""; got != want {
		t.Errorf("NormalizeFilePath(%q) = %q, want %q", "", got, want)
	}
}

func TestNormalizingCommands(t *testing.T) {
	t.Parallel()

	usr, err := user.Current()
	if err != nil {
		t.Errorf("user.Current() error = %v, want nil", err)
	}

	c, err := NormalizeCommand(filepath.Join("~/", "buildkite-agent", "bootstrap.sh"))
	if err != nil {
		t.Errorf("NormalizeCommand(%q) error = %v, want nil", filepath.Join("~/", "buildkite-agent", "bootstrap.sh"), err)
	}
	if got, want := c, filepath.Join(usr.HomeDir, "buildkite-agent", "bootstrap.sh"); got != want {
		t.Errorf("NormalizeCommand(%q) = %q, want %q", filepath.Join("~/", "buildkite-agent", "bootstrap.sh"), got, want)
	}

	c, err = NormalizeCommand("cat test.log")
	if err != nil {
		t.Errorf("NormalizeCommand(%q) error = %v, want nil", "cat test.log", err)
	}
	if got, want := c, "cat test.log"; got != want {
		t.Errorf("NormalizeCommand(%q) = %q, want %q", "cat test.log", got, want)
	}
}
