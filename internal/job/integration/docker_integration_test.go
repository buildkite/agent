package integration

import (
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/internal/job"
	"github.com/buildkite/bintest/v3"
)

func argumentForCommand(cmd string) any {
	// This is unpleasant, but we have to work around the fact that we generate
	// batch scripts for windows and plain commands for everything else
	if runtime.GOOS == "windows" {
		return bintest.MatchPattern("buildkite-script-.+.bat$")
	}
	return cmd
}

func TestRunningCommandWithDocker(t *testing.T) {
	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", job.CommitMetadataKey).
		AndExitWith(0)

	env := []string{
		"BUILDKITE_DOCKER=llamas",
	}

	jobId := "1111-1111-1111-1111"
	imageId := "buildkite_" + jobId + "_image"
	containerId := "buildkite_" + jobId + "_container"

	docker := tester.MustMock(t, "docker")
	docker.ExpectAll([][]any{
		{"build", "-f", "Dockerfile", "-t", imageId, "."},
		{"run", "--name", containerId, imageId, argumentForCommand("true")},
		{"rm", "-f", "-v", containerId},
	})

	expectCommandHooks("0", t, tester)

	tester.RunAndCheck(t, env...)
}

func TestRunningCommandWithDockerAndCustomDockerfile(t *testing.T) {
	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", job.CommitMetadataKey).
		AndExitWith(0)

	env := []string{
		"BUILDKITE_DOCKER=llamas",
		"BUILDKITE_DOCKER_FILE=Dockerfile.llamas",
	}

	jobId := "1111-1111-1111-1111"
	imageId := "buildkite_" + jobId + "_image"
	containerId := "buildkite_" + jobId + "_container"

	docker := tester.MustMock(t, "docker")
	docker.ExpectAll([][]any{
		{"build", "-f", "Dockerfile.llamas", "-t", imageId, "."},
		{"run", "--name", containerId, imageId, argumentForCommand("true")},
		{"rm", "-f", "-v", containerId},
	})

	expectCommandHooks("0", t, tester)

	tester.RunAndCheck(t, env...)
}

func TestRunningFailingCommandWithDocker(t *testing.T) {
	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", job.CommitMetadataKey).
		AndExitWith(0)

	env := []string{
		"BUILDKITE_DOCKER=llamas",
	}

	jobId := "1111-1111-1111-1111"
	imageId := "buildkite_" + jobId + "_image"
	containerId := "buildkite_" + jobId + "_container"

	docker := tester.MustMock(t, "docker")
	docker.ExpectAll([][]any{
		{"build", "-f", "Dockerfile", "-t", imageId, "."},
		{"rm", "-f", "-v", containerId},
	})

	docker.Expect("run", "--name", containerId, imageId, argumentForCommand("true")).
		AndExitWith(1)

	expectCommandHooks("1", t, tester)

	if err = tester.Run(t, env...); err == nil {
		t.Fatalf("tester.Run(t, %v) = %v, want non-nil error", env, err)
	}

	tester.CheckMocks(t)
}

func TestRunningCommandWithDockerCompose(t *testing.T) {
	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", job.CommitMetadataKey).
		AndExitWith(0)

	env := []string{
		"BUILDKITE_DOCKER_COMPOSE_CONTAINER=llamas",
	}

	projectName := "buildkite1111111111111111"

	dockerCompose := tester.MustMock(t, "docker-compose")
	dockerCompose.ExpectAll([][]any{
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "build", "--pull", "llamas"},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "run", "llamas", argumentForCommand("true")},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "kill"},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "rm", "--force", "--all", "-v"},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "down"},
	})

	expectCommandHooks("0", t, tester)

	tester.RunAndCheck(t, env...)
}

func TestRunningFailingCommandWithDockerCompose(t *testing.T) {
	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", job.CommitMetadataKey).
		AndExitWith(0)

	env := []string{
		"BUILDKITE_DOCKER_COMPOSE_CONTAINER=llamas",
	}

	projectName := "buildkite1111111111111111"

	dockerCompose := tester.MustMock(t, "docker-compose")
	dockerCompose.ExpectAll([][]any{
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "build", "--pull", "llamas"},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "kill"},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "rm", "--force", "--all", "-v"},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "down"},
	})

	dockerCompose.Expect("-f", "docker-compose.yml", "-p", projectName, "--verbose", "run", "llamas", argumentForCommand("true")).
		AndWriteToStderr("Nope!").
		AndExitWith(1)

	expectCommandHooks("1", t, tester)

	if err = tester.Run(t, env...); err == nil {
		t.Fatalf("tester.Run(t, %v) = %v, want non-nil error", env, err)
	}

	tester.CheckMocks(t)
}

func TestRunningCommandWithDockerComposeAndExtraConfig(t *testing.T) {
	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", job.CommitMetadataKey).
		AndExitWith(0)

	env := []string{
		"BUILDKITE_DOCKER_COMPOSE_CONTAINER=llamas",
		"BUILDKITE_DOCKER_COMPOSE_FILE=dc1.yml:dc2.yml:dc3.yml",
	}

	projectName := "buildkite1111111111111111"

	dockerCompose := tester.MustMock(t, "docker-compose")
	dockerCompose.ExpectAll([][]any{
		{"-f", "dc1.yml", "-f", "dc2.yml", "-f", "dc3.yml", "-p", projectName, "--verbose", "build", "--pull", "llamas"},
		{"-f", "dc1.yml", "-f", "dc2.yml", "-f", "dc3.yml", "-p", projectName, "--verbose", "run", "llamas", argumentForCommand("true")},
		{"-f", "dc1.yml", "-f", "dc2.yml", "-f", "dc3.yml", "-p", projectName, "--verbose", "kill"},
		{"-f", "dc1.yml", "-f", "dc2.yml", "-f", "dc3.yml", "-p", projectName, "--verbose", "rm", "--force", "--all", "-v"},
		{"-f", "dc1.yml", "-f", "dc2.yml", "-f", "dc3.yml", "-p", projectName, "--verbose", "down"},
	})

	expectCommandHooks("0", t, tester)

	tester.RunAndCheck(t, env...)
}

func TestRunningCommandWithDockerComposeAndBuildAll(t *testing.T) {
	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", job.CommitMetadataKey).
		AndExitWith(0)

	env := []string{
		"BUILDKITE_DOCKER_COMPOSE_CONTAINER=llamas",
		"BUILDKITE_DOCKER_COMPOSE_BUILD_ALL=true",
	}

	dockerCompose := tester.MustMock(t, "docker-compose")
	dockerCompose.IgnoreUnexpectedInvocations()
	dockerCompose.Expect("-f", "docker-compose.yml", "-p", "buildkite1111111111111111", "--verbose", "build", "--pull").Once()

	tester.RunAndCheck(t, env...)
}

func expectCommandHooks(exitStatus string, t *testing.T, tester *ExecutorTester) {
	tester.ExpectGlobalHook("pre-command").Once()
	tester.ExpectLocalHook("pre-command").Once()
	tester.ExpectGlobalHook("post-command").Once()
	tester.ExpectLocalHook("post-command").Once()

	preExitFunc := func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_COMMAND_EXIT_STATUS"), exitStatus; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_COMMAND_EXIT_STATUS) = %q, want %q", got, want)
		}
		c.Exit(0)
	}

	tester.ExpectGlobalHook("pre-exit").Once().AndCallFunc(preExitFunc)
	tester.ExpectLocalHook("pre-exit").Once().AndCallFunc(preExitFunc)
}
