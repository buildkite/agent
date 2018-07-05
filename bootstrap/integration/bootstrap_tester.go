package integration

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
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

	"github.com/buildkite/bintest"
)

// BootstrapTester invokes a buildkite-agent bootstrap script with a temporary environment
type BootstrapTester struct {
	Name       string
	Args       []string
	Env        []string
	HomeDir    string
	PathDir    string
	BuildDir   string
	HooksDir   string
	PluginsDir string
	Repo       *gitRepository
	Output     string

	cmd      *exec.Cmd
	cmdLock  sync.Mutex
	hookMock *bintest.Mock
	mocks    []*bintest.Mock
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

	pluginsDir, err := ioutil.TempDir("", "bootstrap-plugins")
	if err != nil {
		return nil, err
	}

	repo, err := createTestGitRespository()
	if err != nil {
		return nil, err
	}

	bt := &BootstrapTester{
		Name: os.Args[0],
		Args: []string{"bootstrap"},
		Repo: repo,
		Env: []string{
			"HOME=" + homeDir,
			"BUILDKITE_BIN_PATH=" + pathDir,
			"BUILDKITE_BUILD_PATH=" + buildDir,
			"BUILDKITE_HOOKS_PATH=" + hooksDir,
			"BUILDKITE_PLUGINS_PATH=" + pluginsDir,
			`BUILDKITE_REPO=` + repo.Path,
			`BUILDKITE_AGENT_DEBUG=true`,
			`BUILDKITE_AGENT_NAME=test-agent`,
			`BUILDKITE_ORGANIZATION_SLUG=test`,
			`BUILDKITE_PIPELINE_SLUG=test-project`,
			`BUILDKITE_PULL_REQUEST=`,
			`BUILDKITE_PIPELINE_PROVIDER=git`,
			`BUILDKITE_COMMIT=HEAD`,
			`BUILDKITE_BRANCH=master`,
			`BUILDKITE_COMMAND_EVAL=true`,
			`BUILDKITE_ARTIFACT_PATHS=`,
			`BUILDKITE_COMMAND=true`,
			`BUILDKITE_JOB_ID=1111-1111-1111-1111`,
			`BUILDKITE_AGENT_ACCESS_TOKEN=test`,
		},
		PathDir:    pathDir,
		BuildDir:   buildDir,
		HooksDir:   hooksDir,
		PluginsDir: pluginsDir,
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
		return nil, err
	}
	bt.hookMock = hook

	return bt, nil
}

// Mock creates a mock for a binary using bintest
func (b *BootstrapTester) Mock(name string) (*bintest.Mock, error) {
	mock, err := bintest.NewMock(filepath.Join(b.PathDir, name))
	if err != nil {
		return mock, err
	}

	b.mocks = append(b.mocks, mock)
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

// HasMock returns true if a mock has been created by that name
func (b *BootstrapTester) HasMock(name string) bool {
	for _, m := range b.mocks {
		if strings.TrimSuffix(m.Name, filepath.Ext(m.Name)) == name {
			return true
		}
	}
	return false
}

// writeHookScript generates a buildkite-agent hook script that calls a mock binary
func (b *BootstrapTester) writeHookScript(m *bintest.Mock, name string, dir string, args ...string) (string, error) {
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
func (b *BootstrapTester) Run(t *testing.T, env ...string) error {
	// Mock out the meta-data calls to the agent after checkout
	if !b.HasMock("buildkite-agent") {
		agent := b.MustMock(t, "buildkite-agent")
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

	if os.Getenv(`DEBUG_BOOTSTRAP`) == "1" {
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

func (b *BootstrapTester) Cancel() error {
	b.cmdLock.Lock()
	defer b.cmdLock.Unlock()
	log.Printf("Killing pid %d", b.cmd.Process.Pid)
	return b.cmd.Process.Signal(syscall.SIGINT)
}

func (b *BootstrapTester) CheckMocks(t *testing.T) {
	for _, mock := range b.mocks {
		if !mock.Check(t) {
			return
		}
	}
}

func (b *BootstrapTester) CheckoutDir() string {
	return filepath.Join(b.BuildDir, "test-agent", "test", "test-project")
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
	if err := b.Run(t, env...); err != nil {
		t.Logf("Bootstrap output:\n%s", b.Output)
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
	if err := os.RemoveAll(b.PluginsDir); err != nil {
		return err
	}
	return nil
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
			t.Errorf("Error with log writer: %v", err)
			r.CloseWithError(err)
		} else {
			r.Close()
		}
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
