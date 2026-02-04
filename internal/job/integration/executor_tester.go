package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/buildkite/agent/v3/clicommand"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/internal/job"
	"github.com/buildkite/agent/v3/internal/shell"
	"gotest.tools/v3/assert"

	"github.com/buildkite/bintest/v3"
)

// ExecutorTester invokes a buildkite-agent bootstrap script with a temporary environment
type ExecutorTester struct {
	Name          string
	Args          []string
	Env           []string
	HomeDir       string
	PathDir       string
	BuildDir      string
	GitMirrorsDir string
	HooksDir      string
	PluginsDir    string
	Repo          *gitRepository
	Output        string

	cmd      *exec.Cmd
	cmdLock  sync.Mutex
	hookMock *bintest.Mock
	mocks    []*bintest.Mock
}

func NewExecutorTester(ctx context.Context) (*ExecutorTester, error) {
	// The job API experiment adds a unix domain socket to a directory in the home directory
	// UDS names are limited to 108 characters, so we need to use a shorter home directory
	// Who knows what's going on in windowsland
	tmpHomeDir := "/tmp"
	if runtime.GOOS == "windows" {
		tmpHomeDir = ""
	}

	homeDir, err := os.MkdirTemp(tmpHomeDir, "home")
	if err != nil {
		return nil, fmt.Errorf("making home directory: %w", err)
	}

	pathDir, err := os.MkdirTemp("", "bootstrap-path")
	if err != nil {
		return nil, fmt.Errorf("making bootstrap-path directory: %w", err)
	}

	buildDir, err := os.MkdirTemp("", "bootstrap-builds")
	if err != nil {
		return nil, fmt.Errorf("making bootstrap-builds directory: %w", err)
	}

	hooksDir, err := os.MkdirTemp("", "bootstrap-hooks")
	if err != nil {
		return nil, fmt.Errorf("making bootstrap-hooks directory: %w", err)
	}

	pluginsDir, err := os.MkdirTemp("", "bootstrap-plugins")
	if err != nil {
		return nil, fmt.Errorf("making bootstrap-plugins directory: %w", err)
	}

	repo, err := createTestGitRespository()
	if err != nil {
		return nil, fmt.Errorf("creating test git repo: %w", err)
	}

	bt := &ExecutorTester{
		Name: os.Args[0],
		Args: []string{"bootstrap"},
		Repo: repo,
		Env: []string{
			"HOME=" + homeDir,
			"BUILDKITE_BIN_PATH=" + pathDir,
			"BUILDKITE_BUILD_PATH=" + buildDir,
			"BUILDKITE_HOOKS_PATH=" + hooksDir,
			"BUILDKITE_PLUGINS_PATH=" + pluginsDir,
			"BUILDKITE_REPO=" + repo.Path,
			"BUILDKITE_AGENT_DEBUG=true",
			"BUILDKITE_AGENT_NAME=test-agent",
			"BUILDKITE_ORGANIZATION_SLUG=test",
			"BUILDKITE_PIPELINE_SLUG=test-project",
			"BUILDKITE_PULL_REQUEST=",
			"BUILDKITE_PIPELINE_PROVIDER=git",
			"BUILDKITE_COMMIT=HEAD",
			"BUILDKITE_BRANCH=main",
			"BUILDKITE_COMMAND_EVAL=true",
			"BUILDKITE_ARTIFACT_PATHS=",
			"BUILDKITE_COMMAND=true",
			"BUILDKITE_JOB_ID=1111-1111-1111-1111",
			"BUILDKITE_AGENT_ACCESS_TOKEN=test-token-please-ignore",
			fmt.Sprintf("BUILDKITE_REDACTED_VARS=%s", strings.Join(*clicommand.RedactedVars.Value, ",")),
			// Normally the executor will use the self-path to self-execute
			// other subcommands such as 'artifact upload'.
			// Because we've mocked buildkite-agent using bintest, we want it to
			// use the mock instead.
			"BUILDKITE_OVERRIDE_SELF=buildkite-agent",
		},
		PathDir:    pathDir,
		BuildDir:   buildDir,
		HooksDir:   hooksDir,
		PluginsDir: pluginsDir,
	}

	// Support testing experiments
	experiments := experiments.Enabled(ctx)
	if len(experiments) > 0 {
		bt.Env = append(bt.Env, "BUILDKITE_AGENT_EXPERIMENT="+strings.Join(experiments, ","))
	}

	// Windows requires certain env variables to be present
	if runtime.GOOS == "windows" {
		bt.Env = append(bt.Env,
			"PATH="+pathDir+";"+os.Getenv("PATH"),
			"SystemRoot="+os.Getenv("SystemRoot"),
			"WINDIR="+os.Getenv("WINDIR"),
			"COMSPEC="+os.Getenv("COMSPEC"),
			"PATHEXT="+os.Getenv("PATHEXT"),
			"TMP="+os.Getenv("TMP"),
			"TEMP="+os.Getenv("TEMP"),
			"USERPROFILE="+homeDir,
		)
	} else {
		bt.Env = append(bt.Env, "PATH="+pathDir+":"+os.Getenv("PATH"))
	}

	// Create a mock used for hook assertions
	hook, err := bt.Mock("buildkite-agent-hooks")
	if err != nil {
		return nil, fmt.Errorf("mocking buildkite-agent-hooks: %w", err)
	}
	bt.hookMock = hook

	return bt, nil
}

func (e *ExecutorTester) EnableGitMirrors() error {
	gitMirrorsDir, err := os.MkdirTemp("", "bootstrap-git-mirrors")
	if err != nil {
		return fmt.Errorf("making bootstrap-git-mirrors directory: %w", err)
	}

	e.GitMirrorsDir = gitMirrorsDir
	e.Env = append(e.Env, "BUILDKITE_GIT_MIRRORS_PATH="+gitMirrorsDir)

	return nil
}

// Mock creates a mock for a binary using bintest
func (e *ExecutorTester) Mock(name string) (*bintest.Mock, error) {
	mock, err := bintest.NewMock(filepath.Join(e.PathDir, name))
	if err != nil {
		return mock, err
	}

	e.mocks = append(e.mocks, mock)
	return mock, nil
}

// MustMock will fail the test if creating the mock fails
func (e *ExecutorTester) MustMock(t *testing.T, name string) *bintest.Mock {
	t.Helper()
	mock, err := e.Mock(name)
	if err != nil {
		t.Fatalf("ExecutorTester.Mock(%q) error = %v", name, err)
	}
	return mock
}

// HasMock returns true if a mock has been created by that name
func (e *ExecutorTester) HasMock(name string) bool {
	for _, m := range e.mocks {
		if strings.TrimSuffix(m.Name, filepath.Ext(m.Name)) == name {
			return true
		}
	}
	return false
}

// MockAgent creates a mock for the buildkite-agent binary
func (e *ExecutorTester) MockAgent(t *testing.T) *bintest.Mock {
	t.Helper()
	agent := e.MustMock(t, "buildkite-agent")
	agent.Expect("env", "dump").
		Min(0).
		Max(bintest.InfiniteTimes).
		AndCallFunc(mockEnvAsJSONOnStdout(e))

	return agent
}

// writeHookScript generates a buildkite-agent hook script that calls a mock binary
func (e *ExecutorTester) writeHookScript(m *bintest.Mock, name, dir string, args ...string) (string, error) {
	hookScript := filepath.Join(dir, name)
	body := ""

	if runtime.GOOS == "windows" {
		body = fmt.Sprintf("@\"%s\" %s", m.Path, strings.Join(args, " "))
		hookScript += ".bat"
	} else {
		body = "#!/bin/sh\n" + strings.Join(append([]string{m.Path}, args...), " ")
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}

	return hookScript, os.WriteFile(hookScript, []byte(body), 0o600)
}

// ExpectLocalHook creates a mock object and a script in the git repository's buildkite hooks dir
// that proxies to the mock. This lets you set up expectations on a local  hook
func (e *ExecutorTester) ExpectLocalHook(name string) *bintest.Expectation {
	hooksDir := filepath.Join(e.Repo.Path, ".buildkite", "hooks")

	if err := os.MkdirAll(hooksDir, 0o700); err != nil {
		panic(err)
	}

	hookPath, err := e.writeHookScript(e.hookMock, name, hooksDir, "local", name)
	if err != nil {
		panic(err)
	}

	if err = e.Repo.Add(hookPath); err != nil {
		panic(err)
	}
	if err = e.Repo.Commit("Added local hook file %s", name); err != nil {
		panic(err)
	}

	return e.hookMock.Expect("local", name)
}

// ExpectGlobalHook creates a mock object and a script in the global buildkite hooks dir
// that proxies to the mock. This lets you set up expectations on a global hook
func (e *ExecutorTester) ExpectGlobalHook(name string) *bintest.Expectation {
	_, err := e.writeHookScript(e.hookMock, name, e.HooksDir, "global", name)
	if err != nil {
		panic(err)
	}

	return e.hookMock.Expect("global", name)
}

// Run the bootstrap and return any errors
func (e *ExecutorTester) Run(t *testing.T, env ...string) error {
	t.Helper()

	// Mock out the meta-data calls to the agent after checkout
	if !e.HasMock("buildkite-agent") {
		agent := e.MockAgent(t)
		agent.
			Expect("meta-data", "exists", job.CommitMetadataKey).
			Optionally().
			AndExitWith(0)
	}

	path, err := exec.LookPath(e.Name)
	if err != nil {
		return err
	}

	e.cmdLock.Lock()
	e.cmd = exec.Command(path, e.Args...)

	buf := &buffer{}

	if os.Getenv("DEBUG_BOOTSTRAP") == "1" {
		w := newTestLogWriter(t)
		e.cmd.Stdout = io.MultiWriter(buf, w)
		e.cmd.Stderr = io.MultiWriter(buf, w)
	} else {
		e.cmd.Stdout = buf
		e.cmd.Stderr = buf
	}

	e.cmd.Env = append(e.Env, env...)

	err = e.cmd.Start()
	if err != nil {
		e.cmdLock.Unlock()
		return err
	}

	e.cmdLock.Unlock()

	err = e.cmd.Wait()
	e.Output = buf.String()
	return err
}

func (e *ExecutorTester) Cancel() error {
	e.cmdLock.Lock()
	defer e.cmdLock.Unlock()
	log.Printf("Killing pid %d", e.cmd.Process.Pid)
	return e.cmd.Process.Signal(syscall.SIGINT)
}

func (e *ExecutorTester) CheckMocks(t *testing.T) {
	t.Helper()
	for _, mock := range e.mocks {
		mock.Check(t)
	}
}

func (e *ExecutorTester) CheckoutDir() string {
	return filepath.Join(e.BuildDir, "test-agent", "test", "test-project")
}

func (e *ExecutorTester) ReadEnvFromOutput(key string) (string, bool) {
	re := regexp.MustCompile(key + "=(.+)\n")
	matches := re.FindStringSubmatch(e.Output)
	if len(matches) == 0 {
		return "", false
	}
	return matches[1], true
}

// Run the bootstrap and then check the mocks
func (e *ExecutorTester) RunAndCheck(t *testing.T, env ...string) {
	t.Helper()

	if err := e.Run(t, env...); shell.ExitCode(err) != 0 {
		assert.NilError(t, err, "bootstrap output:\n%s", e.Output)
	}

	e.CheckMocks(t)
}

// Close the tester, delete all the directories and mocks
func (e *ExecutorTester) Close() error {
	for _, mock := range e.mocks {
		if err := mock.Close(); err != nil {
			return err
		}
	}
	if e.Repo != nil {
		if err := e.Repo.Close(); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(e.HomeDir); err != nil {
		return err
	}
	if err := os.RemoveAll(e.BuildDir); err != nil {
		return err
	}
	if err := os.RemoveAll(e.HooksDir); err != nil {
		return err
	}
	if err := os.RemoveAll(e.PathDir); err != nil {
		return err
	}
	if err := os.RemoveAll(e.PluginsDir); err != nil {
		return err
	}
	if e.GitMirrorsDir != "" {
		if err := os.RemoveAll(e.GitMirrorsDir); err != nil {
			return err
		}
	}
	return nil
}

func mockEnvAsJSONOnStdout(e *ExecutorTester) func(c *bintest.Call) {
	return func(c *bintest.Call) {
		envMap := map[string]string{}

		for _, e := range e.Env { // The env from the bootstrap tester
			k, v, _ := env.Split(e)
			envMap[k] = v
		}

		for _, e := range c.Env { // The env from the call
			k, v, _ := env.Split(e)
			envMap[k] = v
		}

		envJSON, err := json.Marshal(envMap)
		if err != nil {
			fmt.Println("Failed to marshal env map in mocked agent call:", err)
			c.Exit(1)
		}

		c.Stdout.Write(envJSON)
		c.Exit(0)
	}
}

type testLogWriter struct {
	io.Writer
	sync.Mutex
}

func newTestLogWriter(t *testing.T) *testLogWriter {
	t.Helper()

	r, w := io.Pipe()
	in := bufio.NewScanner(r)
	lw := &testLogWriter{Writer: w}

	go func() {
		for in.Scan() {
			lw.Lock()
			t.Logf("%s", in.Text())
			lw.Unlock()
		}

		if err := in.Err(); err != nil {
			t.Errorf("Reading from pipe: %v", err)
			r.CloseWithError(err)
			return
		}
		r.Close()
	}()

	return lw
}

type buffer struct {
	b bytes.Buffer
	m sync.Mutex
}

func (b *buffer) Read(p []byte) (n int, err error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.Read(p)
}

func (b *buffer) Write(p []byte) (n int, err error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.Write(p)
}

func (b *buffer) String() string {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.String()
}
