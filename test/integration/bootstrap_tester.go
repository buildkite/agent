package integration

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lox/bintest"
)

// BootstrapTester invokes a buildkite-agent bootstrap script with a temporary environment
type BootstrapTester struct {
	Name     string
	Args     []string
	Env      []string
	HomeDir  string
	PathDir  string
	BuildDir string
	HooksDir string
	Repo     *GitRepository

	output string
	mocks  []*bintest.Mock
}

func NewBootstrapTester(name string, args ...string) (*BootstrapTester, error) {
	if !filepath.IsAbs(name) {
		var err error
		name, err = filepath.Abs(name)
		if err != nil {
			return nil, err
		}
	}

	homeDir, err := ioutil.TempDir("", "home")
	if err != nil {
		return nil, err
	}

	pathDir, err := ioutil.TempDir("", "bootstrap-path")
	if err != nil {
		return nil, err
	}

	buildDir, err := ioutil.TempDir("", "bootstrap-builds")
	if err != nil {
		return nil, err
	}

	hooksDir, err := ioutil.TempDir("", "bootstrap-hooks")
	if err != nil {
		return nil, err
	}

	bt := &BootstrapTester{
		Name: name,
		Args: args,
		Env: []string{
			"HOME=" + homeDir,
			"PATH=" + pathDir,
			"BUILDKITE_AGENT_DEBUG=true",
			"BUILDKITE_BIN_PATH=" + pathDir,
			"BUILDKITE_BUILD_PATH=" + buildDir,
			"BUILDKITE_HOOKS_PATH=" + hooksDir,
		},
		PathDir:  pathDir,
		BuildDir: buildDir,
		HooksDir: hooksDir,
	}

	if err = bt.linkCommonCommands(); err != nil {
		return nil, err
	}

	return bt, nil
}

func NewBootstrapTesterWithGitRepository(name string, args ...string) (*BootstrapTester, error) {
	bt, err := NewBootstrapTester(name, args...)
	if err != nil {
		return nil, err
	}
	repo, err := NewGitRepository()
	if err != nil {
		return nil, err
	}
	if err = repo.Commit("Initial Commit", "test.txt", "This is a test"); err != nil {
		return nil, err
	}
	bt.Repo = repo
	return bt, nil
}

func (b *BootstrapTester) Link(src, name string) error {
	// Lookup the absolute path if it doesn't exist
	if !filepath.IsAbs(src) {
		var err error
		src, err = exec.LookPath(src)
		if err != nil {
			return err
		}
	}
	// log.Printf("Linking %s to %s", src, filepath.Join(b.PathDir, name))
	return os.Symlink(src, filepath.Join(b.PathDir, name))
}

func (b *BootstrapTester) linkCommonCommands() error {
	if runtime.GOOS != "windows" {
		for _, bin := range []string{
			"ls", "tr", "mkdir", "cp", "sed", "basename", "uname", "chmod",
			"touch", "env", "grep", "sort", "cat",
		} {
			if err := b.Link(bin, bin); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *BootstrapTester) Mock(name string, t *testing.T) *bintest.Mock {
	mock, err := bintest.NewMock(name)
	if err != nil {
		t.Fatal(err)
	}

	b.mocks = append(b.mocks, mock)

	// move the mock into our path
	if err := os.Rename(mock.Path, filepath.Join(b.PathDir, name)); err != nil {
		t.Fatal(err)
	}

	mock.Path = filepath.Join(b.PathDir, name)
	return mock
}

func (b *BootstrapTester) Hook(name string, t *testing.T) *bintest.Mock {
	if runtime.GOOS == "windows" {
		t.Skip("Hook mocks not supported on windows yet")
		return nil
	}

	mock := b.Mock(name, t)
	hookScript := filepath.Join(b.HooksDir, name)
	body := "#!/bin/sh\n" + mock.Path

	if err := ioutil.WriteFile(hookScript, []byte(body), 0600); err != nil {
		panic(err)
	}

	log.Printf("Writing to %s: %s", hookScript, body)
	return mock
}

func (b *BootstrapTester) Run(env ...string) error {
	log.Printf("Executing %s %v %#v", b.Name, b.Args, env)

	buf := &bytes.Buffer{}

	cmd := exec.Command(b.Name, b.Args...)
	cmd.Stdout = io.MultiWriter(buf, os.Stdout)
	cmd.Stderr = io.MultiWriter(buf, os.Stderr)
	cmd.Env = append(b.Env, env...)

	b.output = buf.String()
	return cmd.Run()
}

func (b *BootstrapTester) AssertOutputContains(t *testing.T, substr string) bool {
	return strings.Contains(b.output, substr)
}

func (b *BootstrapTester) CheckMocksAndClose(t *testing.T) error {
	var checkFailed bool
	for _, mock := range b.mocks {
		if !mock.Check(t) {
			checkFailed = true
		}
	}
	closeErr := b.Close()
	if checkFailed {
		return errors.New("Some mocks failed checks")
	}
	return closeErr
}

func (b *BootstrapTester) Close() error {
	for _, mock := range b.mocks {
		if err := mock.Close(); err != nil {
			return err
		}
	}
	if b.Repo != nil {
		if err := b.Repo.Close(); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(b.HomeDir); err != nil {
		return err
	}
	if err := os.RemoveAll(b.BuildDir); err != nil {
		return err
	}
	if err := os.RemoveAll(b.HooksDir); err != nil {
		return err
	}
	if err := os.RemoveAll(b.PathDir); err != nil {
		return err
	}
	return nil
}
