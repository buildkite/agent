package integration

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lox/bintest/proxy"
)

func jobScriptName(jobId string) string {
	if runtime.GOOS == "windows" {
		return filepath.FromSlash("./buildkite-script-"+jobId) + ".bat"
	}
	return filepath.FromSlash("./buildkite-script-" + jobId)
}

func TestRunningCommandWithDocker(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	env := []string{
		"BUILDKITE_DOCKER=llamas",
	}

	jobId := "1111-1111-1111-1111"
	imageId := "buildkite_" + jobId + "_image"
	containerId := "buildkite_" + jobId + "_container"

	docker := tester.MustMock(t, "docker")
	docker.ExpectAll([][]interface{}{
		{"build", "-f", "Dockerfile", "-t", imageId, "."},
		{"run", "--name", containerId, imageId, jobScriptName(jobId)},
		{"rm", "-f", "-v", containerId},
	})

	expectCommandHooks("0", t, tester)

	tester.RunAndCheck(t, env...)
}

func TestRunningCommandWithDockerAndCustomDockerfile(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	env := []string{
		"BUILDKITE_DOCKER=llamas",
		"BUILDKITE_DOCKER_FILE=Dockerfile.llamas",
	}

	jobId := "1111-1111-1111-1111"
	imageId := "buildkite_" + jobId + "_image"
	containerId := "buildkite_" + jobId + "_container"

	docker := tester.MustMock(t, "docker")
	docker.ExpectAll([][]interface{}{
		{"build", "-f", "Dockerfile.llamas", "-t", imageId, "."},
		{"run", "--name", containerId, imageId, jobScriptName(jobId)},
		{"rm", "-f", "-v", containerId},
	})

	expectCommandHooks("0", t, tester)

	tester.RunAndCheck(t, env...)
}

func TestRunningFailingCommandWithDocker(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	env := []string{
		"BUILDKITE_DOCKER=llamas",
	}

	jobId := "1111-1111-1111-1111"
	imageId := "buildkite_" + jobId + "_image"
	containerId := "buildkite_" + jobId + "_container"

	docker := tester.MustMock(t, "docker")
	docker.ExpectAll([][]interface{}{
		{"build", "-f", "Dockerfile", "-t", imageId, "."},
		{"rm", "-f", "-v", containerId},
	})

	docker.Expect("run", "--name", containerId, imageId, jobScriptName(jobId)).
		AndExitWith(1)

	expectCommandHooks("1", t, tester)

	if err = tester.Run(t, env...); err == nil {
		t.Fatal("Expected bootstrap to fail")
	}

	tester.CheckMocks(t)
}

func TestRunningCommandWithDockerCompose(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	env := []string{
		"BUILDKITE_DOCKER_COMPOSE_CONTAINER=llamas",
	}

	jobId := "1111-1111-1111-1111"
	projectName := "buildkite1111111111111111"

	dockerCompose := tester.MustMock(t, "docker-compose")
	dockerCompose.ExpectAll([][]interface{}{
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "build", "--pull", "llamas"},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "run", "llamas", jobScriptName(jobId)},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "kill"},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "rm", "--force", "--all", "-v"},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "down"},
	})

	expectCommandHooks("0", t, tester)

	tester.RunAndCheck(t, env...)
}

func TestRunningFailingCommandWithDockerCompose(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	env := []string{
		"BUILDKITE_DOCKER_COMPOSE_CONTAINER=llamas",
	}

	jobId := "1111-1111-1111-1111"
	projectName := "buildkite1111111111111111"

	dockerCompose := tester.MustMock(t, "docker-compose")
	dockerCompose.ExpectAll([][]interface{}{
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "build", "--pull", "llamas"},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "kill"},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "rm", "--force", "--all", "-v"},
		{"-f", "docker-compose.yml", "-p", projectName, "--verbose", "down"},
	})

	dockerCompose.Expect("-f", "docker-compose.yml", "-p", projectName, "--verbose", "run", "llamas", jobScriptName(jobId)).
		AndWriteToStderr("Nope!").
		AndExitWith(1)

	expectCommandHooks("1", t, tester)

	if err = tester.Run(t, env...); err == nil {
		t.Fatal("Expected bootstrap to fail")
	}

	tester.CheckMocks(t)
}

func TestRunningCommandWithDockerComposeAndExtraConfig(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	env := []string{
		"BUILDKITE_DOCKER_COMPOSE_CONTAINER=llamas",
		"BUILDKITE_DOCKER_COMPOSE_FILE=dc1.yml:dc2.yml:dc3.yml",
	}

	jobId := "1111-1111-1111-1111"
	projectName := "buildkite1111111111111111"

	dockerCompose := tester.MustMock(t, "docker-compose")
	dockerCompose.ExpectAll([][]interface{}{
		{"-f", "dc1.yml", "-f", "dc2.yml", "-f", "dc3.yml", "-p", projectName, "--verbose", "build", "--pull", "llamas"},
		{"-f", "dc1.yml", "-f", "dc2.yml", "-f", "dc3.yml", "-p", projectName, "--verbose", "run", "llamas", jobScriptName(jobId)},
		{"-f", "dc1.yml", "-f", "dc2.yml", "-f", "dc3.yml", "-p", projectName, "--verbose", "kill"},
		{"-f", "dc1.yml", "-f", "dc2.yml", "-f", "dc3.yml", "-p", projectName, "--verbose", "rm", "--force", "--all", "-v"},
		{"-f", "dc1.yml", "-f", "dc2.yml", "-f", "dc3.yml", "-p", projectName, "--verbose", "down"},
	})

	expectCommandHooks("0", t, tester)

	tester.RunAndCheck(t, env...)
}

func TestRunningCommandWithDockerComposeAndBuildAll(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
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

func expectCommandHooks(exitStatus string, t *testing.T, tester *BootstrapTester) {
	tester.ExpectGlobalHook("pre-command").Once()
	tester.ExpectLocalHook("pre-command").Once()
	tester.ExpectGlobalHook("post-command").Once()
	tester.ExpectLocalHook("post-command").Once()

	preExitFunc := func(c *proxy.Call) {
		cmdExitStatus := c.GetEnv(`BUILDKITE_COMMAND_EXIT_STATUS`)
		if cmdExitStatus != exitStatus {
			t.Errorf("Expected an exit status of %s, got %v", exitStatus, cmdExitStatus)
		}
		c.Exit(0)
	}

	tester.ExpectGlobalHook("pre-exit").Once().AndCallFunc(preExitFunc)
	tester.ExpectLocalHook("pre-exit").Once().AndCallFunc(preExitFunc)
}
