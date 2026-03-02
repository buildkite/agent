package job

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/internal/shell"
)

var dockerEnv = []string{
	"BUILDKITE_DOCKER_COMPOSE_CONTAINER",
	"BUILDKITE_DOCKER_COMPOSE_FILE",
	"BUILDKITE_DOCKER",
	"BUILDKITE_DOCKER_FILE",
	"BUILDKITE_DOCKER_COMPOSE_BUILD_ALL",
	"BUILDKITE_DOCKER_COMPOSE_LEAVE_VOLUMES",
}

func hasDeprecatedDockerIntegration(sh *shell.Shell) bool {
	return slices.ContainsFunc(dockerEnv, sh.Env.Exists)
}

func runDeprecatedDockerIntegration(ctx context.Context, sh *shell.Shell, cmd []string) error {
	warnNotSet := func(k1, k2 string) {
		sh.Warningf("%s is set, but without %s, which it requires. You should be able to safely remove this from your pipeline.", k1, k2)
	}

	switch {
	case sh.Env.Exists("BUILDKITE_DOCKER_COMPOSE_CONTAINER"):
		sh.Warningf("BUILDKITE_DOCKER_COMPOSE_CONTAINER is set, which is deprecated in Agent v3 and will be removed in v4. Consider using the :docker: docker-compose plugin instead at https://github.com/buildkite-plugins/docker-compose-buildkite-plugin.")
		return runDockerComposeCommand(ctx, sh, cmd)

	case sh.Env.Exists("BUILDKITE_DOCKER"):
		sh.Warningf("BUILDKITE_DOCKER is set, which is deprecated in Agent v3 and will be removed in v4. Consider using the docker plugin instead at https://github.com/buildkite-plugins/docker-buildkite-plugin.")
		return runDockerCommand(ctx, sh, cmd)

	case sh.Env.Exists("BUILDKITE_DOCKER_COMPOSE_FILE"):
		warnNotSet("BUILDKITE_DOCKER_COMPOSE_FILE", "BUILDKITE_DOCKER_COMPOSE_CONTAINER")

	case sh.Env.Exists("BUILDKITE_DOCKER_COMPOSE_BUILD_ALL"):
		warnNotSet("BUILDKITE_DOCKER_COMPOSE_BUILD_ALL", "BUILDKITE_DOCKER_COMPOSE_CONTAINER")

	case sh.Env.Exists("BUILDKITE_DOCKER_COMPOSE_LEAVE_VOLUMES"):
		warnNotSet("BUILDKITE_DOCKER_COMPOSE_LEAVE_VOLUMES", "BUILDKITE_DOCKER_COMPOSE_CONTAINER")

	case sh.Env.Exists("BUILDKITE_DOCKER_COMPOSE_LEAVE_VOLUMES"):
		warnNotSet("BUILDKITE_DOCKER_COMPOSE_LEAVE_VOLUMES", "BUILDKITE_DOCKER_COMPOSE_CONTAINER")
	}

	return errors.New("failed to find any docker env")
}

func tearDownDeprecatedDockerIntegration(ctx context.Context, sh *shell.Shell) error {
	if container, ok := sh.Env.Get("DOCKER_CONTAINER"); ok {
		sh.Printf("~~~ Cleaning up Docker containers")

		if err := sh.Command("docker", "rm", "-f", "-v", container).Run(ctx); err != nil {
			return err
		}
	} else if projectName, ok := sh.Env.Get("COMPOSE_PROJ_NAME"); ok {
		sh.Printf("~~~ Cleaning up Docker containers")

		// Friendly kill
		_ = runDockerCompose(ctx, sh, projectName, "kill")

		if sh.Env.GetBool("BUILDKITE_DOCKER_COMPOSE_LEAVE_VOLUMES", false) {
			_ = runDockerCompose(ctx, sh, projectName, "rm", "--force", "--all")
		} else {
			_ = runDockerCompose(ctx, sh, projectName, "rm", "--force", "--all", "-v")
		}

		return runDockerCompose(ctx, sh, projectName, "down")
	}

	return nil
}

// runDockerCommand executes a command inside a docker container that is built as needed
// Ported from https://github.com/buildkite/agent/blob/2b8f1d569b659e07de346c0e3ae7090cb98e49ba/templates/bootstrap.sh#L439
func runDockerCommand(ctx context.Context, sh *shell.Shell, cmd []string) error {
	jobId, _ := sh.Env.Get("BUILDKITE_JOB_ID")
	dockerContainer := fmt.Sprintf("buildkite_%s_container", jobId)
	dockerImage := fmt.Sprintf("buildkite_%s_image", jobId)

	dockerFile, _ := sh.Env.Get("BUILDKITE_DOCKER_FILE")
	if dockerFile == "" {
		dockerFile = "Dockerfile"
	}

	sh.Env.Set("DOCKER_CONTAINER", dockerContainer)
	sh.Env.Set("DOCKER_IMAGE", dockerImage)

	sh.Printf("~~~ :docker: Building Docker image %s", dockerImage)
	shCmd := sh.Command("docker", "build", "-f", dockerFile, "-t", dockerImage, ".")
	if err := shCmd.Run(ctx); err != nil {
		return err
	}

	sh.Headerf(":docker: Running command (in Docker container)")
	shCmd = sh.Command("docker", append([]string{"run", "--name", dockerContainer, dockerImage}, cmd...)...)
	if err := shCmd.Run(ctx); err != nil {
		return err
	}

	return nil
}

// runDockerComposeCommand executes a command with docker-compose
// Ported from https://github.com/buildkite/agent/blob/2b8f1d569b659e07de346c0e3ae7090cb98e49ba/templates/bootstrap.sh#L462
func runDockerComposeCommand(ctx context.Context, sh *shell.Shell, cmd []string) error {
	composeContainer, _ := sh.Env.Get("BUILDKITE_DOCKER_COMPOSE_CONTAINER")
	jobId, _ := sh.Env.Get("BUILDKITE_JOB_ID")

	// Compose strips dashes and underscores, so we'll remove them
	// to match the docker container names
	projectName := strings.ReplaceAll("buildkite"+jobId, "-", "")

	sh.Env.Set("COMPOSE_PROJ_NAME", projectName)
	sh.Headerf(":docker: Building Docker images")

	if sh.Env.GetBool("BUILDKITE_DOCKER_COMPOSE_BUILD_ALL", false) {
		if err := runDockerCompose(ctx, sh, projectName, "build", "--pull"); err != nil {
			return err
		}
	} else {
		if err := runDockerCompose(ctx, sh, projectName, "build", "--pull", composeContainer); err != nil {
			return err
		}
	}

	sh.Headerf(":docker: Running command (in Docker Compose container)")
	return runDockerCompose(ctx, sh, projectName, append([]string{"run", composeContainer}, cmd...)...)
}

func runDockerCompose(ctx context.Context, sh *shell.Shell, projectName string, commandArgs ...string) error {
	args := []string{}

	composeFile, _ := sh.Env.Get("BUILDKITE_DOCKER_COMPOSE_FILE")
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}

	// composeFile might be multiple files, spaces or colons
	for chunk := range strings.FieldsSeq(composeFile) {
		for file := range strings.SplitSeq(chunk, ":") {
			args = append(args, "-f", file)
		}
	}

	args = append(args, "-p", projectName)

	if sh.Env.GetBool("BUILDKITE_AGENT_DEBUG", false) {
		args = append(args, "--verbose")
	}

	args = append(args, commandArgs...)
	return sh.Command("docker-compose", args...).Run(ctx)
}
