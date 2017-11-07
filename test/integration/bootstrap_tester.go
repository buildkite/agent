package integration

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	Output   string

	hookMock *bintest.Mock
	mocks    []*bintest.Mock
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

	if err = bt.LinkCommonCommands(); err != nil {
		return nil, err
	}

	// Create a mock used for hook assertions
	hook, err := bt.Mock("buildkite-agent-hooks")
	if err != nil {
		return nil, err
	}
	bt.hookMock = hook

	return bt, nil
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

// Link common commands from system path, these can be mocked as needed
func (b *BootstrapTester) LinkCommonCommands() error {
	if runtime.GOOS != "windows" {
		for _, bin := range []string{
			"ls", "tr", "mkdir", "cp", "sed", "basename", "uname", "chmod", "rm",
			"touch", "env", "grep", "sort", "cat", "true", "git", "ssh-keygen", "ssh-keyscan",
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

// MustMock will fail the test if creating the mock fails
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

// ExpectLocalHook creates a mock object and a script in the git repository's buildkite hooks dir
// that proxies to the mock. This lets you set up expectations on a local  hook
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

// ExpectGlobalHook creates a mock object and a script in the global buildkite hooks dir
// that proxies to the mock. This lets you set up expectations on a global hook
func (b *BootstrapTester) ExpectGlobalHook(name string) *bintest.Expectation {
	_, err := b.writeHookScript(b.hookMock, name, b.HooksDir, "global", name)
	if err != nil {
		panic(err)
	}

	return b.hookMock.Expect("global", name)
}

// Run the bootstrap and return any errors
func (b *BootstrapTester) Run(env ...string) error {
	buf := &bytes.Buffer{}
	cmd := exec.Command(b.Name, b.Args...)
	cmd.Stdout = io.MultiWriter(buf, os.Stdout)
	cmd.Stderr = io.MultiWriter(buf, os.Stderr)
	cmd.Env = append(b.Env, env...)

	err := cmd.Run()
	b.Output = buf.String()
	return err
}

func (b *BootstrapTester) CheckMocks(t *testing.T) {
	for _, mock := range b.mocks {
		if mock.Check(t) {
			t.Logf("Mock %s passed checks", mock.Name)
		}
	}
}

func (b *BootstrapTester) CheckoutDir() string {
	return filepath.Join(b.BuildDir, "test-agent", "test-project")
}

func (b *BootstrapTester) ReadEnvFromOutput(key string) (string, bool) {
	re := regexp.MustCompile(key + "=(.+)\n")
	matches := re.FindStringSubmatch(b.Output)
	if len(matches) == 0 {
		return "", false
	}
	return matches[1], true
}

// Run the bootstrap and then check the mocks
func (b *BootstrapTester) RunAndCheck(t *testing.T, env ...string) {
	if err := b.Run(env...); err != nil {
		t.Fatal(err)
	}
	b.CheckMocks(t)
}

// Close the tester, delete all the directories and mocks
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
