package integration

import (
	"bufio"
	"bytes"
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

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/experiments"

	"github.com/buildkite/bintest/v3"
)

// ExecutorTester invokes a buildkite-agent executor script with a temporary environment
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

func NewExecutorTester() (*ExecutorTester, error) {
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

	pathDir, err := os.MkdirTemp("", "executor-path")
	if err != nil {
		return nil, fmt.Errorf("making executor-path directory: %w", err)
	}

	buildDir, err := os.MkdirTemp("", "executor-builds")
	if err != nil {
		return nil, fmt.Errorf("making executor-builds directory: %w", err)
	}

	hooksDir, err := os.MkdirTemp("", "executor-hooks")
	if err != nil {
		return nil, fmt.Errorf("making executor-hooks directory: %w", err)
	}

	pluginsDir, err := os.MkdirTemp("", "executor-plugins")
	if err != nil {
		return nil, fmt.Errorf("making executor-plugins directory: %w", err)
	}

	repo, err := createTestGitRespository()
	if err != nil {
		return nil, fmt.Errorf("creating test git repo: %w", err)
	}

	bt := &ExecutorTester{
		Name: os.Args[0],
		Args: []string{"job", "run"},
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
			"BUILDKITE_AGENT_ACCESS_TOKEN=test",
		},
		PathDir:    pathDir,
		BuildDir:   buildDir,
		HooksDir:   hooksDir,
		PluginsDir: pluginsDir,
	}

	// Support testing experiments
	if exp := experiments.Enabled(); len(exp) > 0 {
		bt.Env = append(bt.Env, "BUILDKITE_AGENT_EXPERIMENT="+strings.Join(exp, ","))

		if experiments.IsEnabled(experiments.GitMirrors) {
			gitMirrorsDir, err := os.MkdirTemp("", "executor-git-mirrors")
			if err != nil {
				return nil, fmt.Errorf("making executor-git-mirrors directory: %w", err)
			}

			bt.GitMirrorsDir = gitMirrorsDir
			bt.Env = append(bt.Env, "BUILDKITE_GIT_MIRRORS_PATH="+gitMirrorsDir)
		}
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
		)
	} else {
		bt.Env = append(bt.Env,
			"PATH="+pathDir+":"+os.Getenv("PATH"),
		)
	}

	// Create a mock used for hook assertions
	hook, err := bt.Mock("buildkite-agent-hooks")
	if err != nil {
		return nil, fmt.Errorf("mocking buildkite-agent-hooks: %w", err)
	}
	bt.hookMock = hook

	return bt, nil
}

// Mock creates a mock for a binary using bintest
func (b *ExecutorTester) Mock(name string) (*bintest.Mock, error) {
	mock, err := bintest.NewMock(filepath.Join(b.PathDir, name))
	if err != nil {
		return mock, err
	}

	b.mocks = append(b.mocks, mock)
	return mock, nil
}

// MustMock will fail the test if creating the mock fails
func (b *ExecutorTester) MustMock(t *testing.T, name string) *bintest.Mock {
	mock, err := b.Mock(name)
	if err != nil {
		t.Fatalf("ExecutorTester.Mock(%q) error = %v", name, err)
	}
	return mock
}

// HasMock returns true if a mock has been created by that name
func (b *ExecutorTester) HasMock(name string) bool {
	for _, m := range b.mocks {
		if strings.TrimSuffix(m.Name, filepath.Ext(m.Name)) == name {
			return true
		}
	}
	return false
}

// MockAgent creates a mock for the buildkite-agent binary
func (b *ExecutorTester) MockAgent(t *testing.T) *bintest.Mock {
	agent := b.MustMock(t, "buildkite-agent")
	agent.Expect("env", "dump").
		Min(0).
		Max(bintest.InfiniteTimes).
		AndCallFunc(mockEnvAsJSONOnStdout(b))

	return agent
}

// writeHookScript generates a buildkite-agent hook script that calls a mock binary
func (b *ExecutorTester) writeHookScript(m *bintest.Mock, name string, dir string, args ...string) (string, error) {
	hookScript := filepath.Join(dir, name)
	body := ""

	if runtime.GOOS == "windows" {
		body = fmt.Sprintf("@\"%s\" %s", m.Path, strings.Join(args, " "))
		hookScript += ".bat"
	} else {
		body = "#!/bin/sh\n" + strings.Join(append([]string{m.Path}, args...), " ")
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}

	return hookScript, os.WriteFile(hookScript, []byte(body), 0600)
}

// ExpectLocalHook creates a mock object and a script in the git repository's buildkite hooks dir
// that proxies to the mock. This lets you set up expectations on a local  hook
func (b *ExecutorTester) ExpectLocalHook(name string) *bintest.Expectation {
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
func (b *ExecutorTester) ExpectGlobalHook(name string) *bintest.Expectation {
	_, err := b.writeHookScript(b.hookMock, name, b.HooksDir, "global", name)
	if err != nil {
		panic(err)
	}

	return b.hookMock.Expect("global", name)
}

// Run the executor and return any errors
func (b *ExecutorTester) Run(t *testing.T, env ...string) error {
	// Mock out the meta-data calls to the agent after checkout
	if !b.HasMock("buildkite-agent") {
		agent := b.MockAgent(t)
		agent.
			Expect("meta-data", "exists", "buildkite:git:commit").
			Optionally().
			AndExitWith(0)
	}

	path, err := exec.LookPath(b.Name)
	if err != nil {
		return err
	}

	b.cmdLock.Lock()
	b.cmd = exec.Command(path, b.Args...)

	buf := &buffer{}

	if os.Getenv("DEBUG_JOB_EXEC") == "1" {
		w := newTestLogWriter(t)
		b.cmd.Stdout = io.MultiWriter(buf, w)
		b.cmd.Stderr = io.MultiWriter(buf, w)
	} else {
		b.cmd.Stdout = buf
		b.cmd.Stderr = buf
	}

	b.cmd.Env = append(b.Env, env...)

	err = b.cmd.Start()
	if err != nil {
		b.cmdLock.Unlock()
		return err
	}

	b.cmdLock.Unlock()

	err = b.cmd.Wait()
	b.Output = buf.String()
	return err
}

func (b *ExecutorTester) Cancel() error {
	b.cmdLock.Lock()
	defer b.cmdLock.Unlock()
	log.Printf("Killing pid %d", b.cmd.Process.Pid)
	return b.cmd.Process.Signal(syscall.SIGINT)
}

func (b *ExecutorTester) CheckMocks(t *testing.T) {
	for _, mock := range b.mocks {
		mock.Check(t)
	}
}

func (b *ExecutorTester) CheckoutDir() string {
	return filepath.Join(b.BuildDir, "test-agent", "test", "test-project")
}

func (b *ExecutorTester) ReadEnvFromOutput(key string) (string, bool) {
	re := regexp.MustCompile(key + "=(.+)\n")
	matches := re.FindStringSubmatch(b.Output)
	if len(matches) == 0 {
		return "", false
	}
	return matches[1], true
}

// Run the executor and then check the mocks
func (b *ExecutorTester) RunAndCheck(t *testing.T, env ...string) {
	err := b.Run(t, env...)
	t.Logf("Executor output:\n%s", b.Output)

	if err != nil {
		t.Fatalf("ExecutorTester.Run(%q) = %v", env, err)
	}

	b.CheckMocks(t)
}

// Close the tester, delete all the directories and mocks
func (b *ExecutorTester) Close() error {
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
	if err := os.RemoveAll(b.PluginsDir); err != nil {
		return err
	}
	if b.GitMirrorsDir != "" {
		if err := os.RemoveAll(b.GitMirrorsDir); err != nil {
			return err
		}
	}
	return nil
}

func mockEnvAsJSONOnStdout(b *ExecutorTester) func(c *bintest.Call) {
	return func(c *bintest.Call) {
		envMap := map[string]string{}

		for _, e := range b.Env { // The env from the executor tester
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
