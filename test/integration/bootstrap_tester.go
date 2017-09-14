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
	Repo     *gitRepository

	// mocks that are referenced internally
	hookMock, agentMock *bintest.Mock

	hasCheckoutHook bool
	output          string
	mocks           []*bintest.Mock
}

func bootstrapPath() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "templates/bootstrap.sh")
}

func NewBootstrapTester() (*BootstrapTester, error) {
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

	repo, err := createTestGitRespository()
	if err != nil {
		return nil, err
	}

	bt := &BootstrapTester{
		Name: bootstrapPath(),
		Repo: repo,
		Env: []string{
			"HOME=" + homeDir,
			"PATH=" + pathDir,
			"BUILDKITE_BIN_PATH=" + pathDir,
			"BUILDKITE_BUILD_PATH=" + buildDir,
			"BUILDKITE_HOOKS_PATH=" + hooksDir,
			`BUILDKITE_REPO=` + repo.Path,
			`BUILDKITE_AGENT_DEBUG=true`,
			`BUILDKITE_AGENT_NAME=test-agent`,
			`BUILDKITE_PROJECT_SLUG=test-project`,
			`BUILDKITE_PULL_REQUEST=`,
			`BUILDKITE_PROJECT_PROVIDER=git`,
			`BUILDKITE_COMMIT=HEAD`,
			`BUILDKITE_BRANCH=master`,
			`BUILDKITE_COMMAND_EVAL=true`,
			`BUILDKITE_ARTIFACT_PATHS=`,
			`BUILDKITE_COMMAND=true`,
			`BUILDKITE_JOB_ID=1111-1111-1111-1111`,
		},
		PathDir:  pathDir,
		BuildDir: buildDir,
		HooksDir: hooksDir,
	}

	if err = bt.LinkPosixCommands(); err != nil {
		return nil, err
	}

	// Bake in some always used mocks

	agent, err := bt.Mock("buildkite-agent")
	if err != nil {
		return nil, err
	}

	bt.agentMock = agent

	hook, err := bt.Mock("buildkite-agent-hooks")
	if err != nil {
		return nil, err
	}

	bt.hookMock = hook

	return bt, nil
}

// The agent has a series of metadata commands for handling internal buildkite metadata
// this mocks them out entirely
func expectBuildkiteAgentCheckoutMetadataCommands(agent *bintest.Mock) {
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(1)
	agent.
		Expect("meta-data", "set", "buildkite:git:commit", bintest.MatchAny()).
		AndExitWith(0)
	agent.
		Expect("meta-data", "set", "buildkite:git:branch", bintest.MatchAny()).
		AndExitWith(0)
}

// LinkLocalCommand creates a symlink for commands into the tester PATH
func (b *BootstrapTester) LinkLocalCommand(name string) error {
	if !filepath.IsAbs(name) {
		var err error
		name, err = exec.LookPath(name)
		if err != nil {
			return err
		}
	}
	return os.Symlink(name, filepath.Join(b.PathDir, filepath.Base(name)))
}

// Link common posix commands, these aren't worth mocking
func (b *BootstrapTester) LinkPosixCommands() error {
	if runtime.GOOS != "windows" {
		for _, bin := range []string{
			"ls", "tr", "mkdir", "cp", "sed", "basename", "uname", "chmod",
			"touch", "env", "grep", "sort", "cat", "true",
		} {
			if err := b.LinkLocalCommand(bin); err != nil {
				return err
			}
		}
	}
	return nil
}

// Mock creates a mock for a binary using bintest
func (b *BootstrapTester) Mock(name string) (*bintest.Mock, error) {
	mock, err := bintest.NewMock(name)
	if err != nil {
		return mock, err
	}

	b.mocks = append(b.mocks, mock)

	// move the mock into our path
	if err := os.Rename(mock.Path, filepath.Join(b.PathDir, name)); err != nil {
		return mock, err
	}

	mock.Path = filepath.Join(b.PathDir, name)
	return mock, err
}

func (b *BootstrapTester) MustMock(t *testing.T, name string) *bintest.Mock {
	mock, err := b.Mock(name)
	if err != nil {
		t.Fatal(err)
	}
	return mock
}

// writeHookScript generates a buildkite-agent hook script that calls a mock binary
func (b *BootstrapTester) writeHookScript(m *bintest.Mock, name string, dir string, args ...string) (string, error) {
	// TODO: support windows tests
	hookScript := filepath.Join(dir, name)
	body := "#!/bin/sh\n" + strings.Join(append([]string{m.Path}, args...), " ")

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}

	return hookScript, ioutil.WriteFile(hookScript, []byte(body), 0600)
}

func (b *BootstrapTester) ExpectLocalHook(name string) *bintest.Expectation {
	hooksDir := filepath.Join(b.Repo.Path, ".buildkite", "hooks")

	if err := os.MkdirAll(hooksDir, 0700); err != nil {
		panic(err)
	}

	hookPath, err := b.writeHookScript(b.hookMock, name, hooksDir, "local", name)
	if err != nil {
		panic(err)
	}

	if err = b.Repo.Add(hookPath); err != nil {
		panic(err)
	}
	if err = b.Repo.Commit("Added local hook file %s", name); err != nil {
		panic(err)
	}

	return b.hookMock.Expect("local", name)
}

func (b *BootstrapTester) ExpectGlobalHook(name string) *bintest.Expectation {
	_, err := b.writeHookScript(b.hookMock, name, b.HooksDir, "global", name)
	if err != nil {
		panic(err)
	}

	if name == "checkout" {
		b.hasCheckoutHook = true
	}

	return b.hookMock.Expect("global", name)
}

func (b *BootstrapTester) Run(env ...string) error {
	if !b.hasCheckoutHook {
		expectBuildkiteAgentCheckoutMetadataCommands(b.agentMock)
	}

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
