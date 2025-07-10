package container

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/process"
)

// waitStatus implements the process.WaitStatus interface
type waitStatus struct {
	exitCode int
}

func (ws *waitStatus) ExitStatus() int {
	return ws.exitCode
}

func (ws *waitStatus) Signaled() bool {
	return false
}

func (ws *waitStatus) Signal() syscall.Signal {
	return syscall.Signal(0)
}

// Runner executes Buildkite jobs inside Docker containers
type Runner struct {
	logger logger.Logger
	config RunnerConfig

	// Docker state
	containerID string
	imageName   string

	// Process state
	exitCode int
	finished chan struct{}
	started  chan struct{}
	mu       sync.RWMutex
}

// RunnerConfig defines the configuration for the container runner
type RunnerConfig struct {
	// Image is the Docker image to run (from BUILDKITE_IMAGE env var)
	Image string

	// WorkingDir is the host directory to mount into the container
	WorkingDir string

	// Env is the environment variables to pass to the container
	Env []string

	// Command is the command to run inside the container
	Command string

	// Args are the arguments to pass to the command
	Args []string

	// Stdout is where to write stdout
	Stdout io.Writer

	// Stderr is where to write stderr
	Stderr io.Writer

	// InterruptSignal is the signal to send to interrupt the process
	InterruptSignal process.Signal

	// SignalGracePeriod is how long to wait for graceful shutdown
	SignalGracePeriod time.Duration
}

// NewRunner creates a new container runner
func NewRunner(logger logger.Logger, config RunnerConfig) *Runner {
	return &Runner{
		logger:   logger,
		config:   config,
		finished: make(chan struct{}),
		started:  make(chan struct{}),
	}
}

// Start implements the process.Process interface
func (r *Runner) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Set defaults
	if r.config.InterruptSignal == 0 {
		r.config.InterruptSignal = process.SIGTERM
	}
	if r.config.SignalGracePeriod == 0 {
		r.config.SignalGracePeriod = 30 * time.Second
	}

	r.imageName = r.config.Image

	// Pull the Docker image first
	if err := r.pullImage(ctx); err != nil {
		return fmt.Errorf("failed to pull Docker image: %w", err)
	}

	// Run the container directly (instead of create + start)
	go r.runContainer(ctx)

	// Signal that the process has started
	close(r.started)

	return nil
}

// pullImage pulls the Docker image
func (r *Runner) pullImage(ctx context.Context) error {
	r.logger.Info("Pulling Docker image: %s", r.imageName)

	cmd := exec.CommandContext(ctx, "docker", "pull", r.imageName)
	cmd.Stdout = r.config.Stdout
	cmd.Stderr = r.config.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker pull failed: %w", err)
	}

	r.logger.Info("Successfully pulled Docker image: %s", r.imageName)
	return nil
}

// runContainer runs the Docker container and captures output/exit code directly
func (r *Runner) runContainer(ctx context.Context) {
	defer close(r.finished)
	defer r.cleanup()

	// Generate a unique container name
	r.containerID = fmt.Sprintf("buildkite-job-%d", time.Now().UnixNano())

	// Build docker run command
	args := []string{
		"run",
		"--name", r.containerID,
		"--rm", // Remove container when it exits
		"--workdir", "/buildkite/build",
	}

	// Mount the working directory
	if r.config.WorkingDir != "" {
		mountArg := fmt.Sprintf("%s:/buildkite/build", r.config.WorkingDir)
		args = append(args, "-v", mountArg)
	}

	// Add environment variables
	for _, env := range r.config.Env {
		args = append(args, "-e", env)
	}

	// Add image
	args = append(args, r.imageName)

	// Add command and args
	if r.config.Command != "" {
		args = append(args, r.config.Command)
		args = append(args, r.config.Args...)
	}

	r.logger.Debug("Running container with command: docker %s", strings.Join(args, " "))

	// Run the container with direct output streaming
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = r.config.Stdout
	cmd.Stderr = r.config.Stderr

	// Run and capture exit code
	err := cmd.Run()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			r.exitCode = exitError.ExitCode()
		} else {
			r.exitCode = 1
		}
		r.logger.Info("Container %s finished with exit code: %d", r.containerID, r.exitCode)
	} else {
		r.exitCode = 0
		r.logger.Info("Container %s finished successfully", r.containerID)
	}
}

// cleanup removes the container (though --rm flag should handle this automatically)
func (r *Runner) cleanup() {
	if r.containerID == "" {
		return
	}

	r.logger.Debug("Container cleanup for: %s (--rm flag should handle removal automatically)", r.containerID)

	// With --rm flag, the container should remove itself automatically
	// Only need to stop if it's still running (though it shouldn't be)
	stopCmd := exec.Command("docker", "stop", r.containerID)
	if err := stopCmd.Run(); err != nil {
		// This is expected if container already finished
		r.logger.Debug("Container stop failed (container likely already finished): %v", err)
	}
}

// Wait waits for the container to finish
func (r *Runner) Wait() error {
	<-r.finished
	return nil
}

// WaitResult waits for the container to finish and returns the result
func (r *Runner) WaitResult() error {
	<-r.finished
	if r.exitCode != 0 {
		return fmt.Errorf("container exited with code %d", r.exitCode)
	}
	return nil
}

// Done returns a channel that closes when the process finishes
func (r *Runner) Done() <-chan struct{} {
	return r.finished
}

// Started returns a channel that closes when the process starts
func (r *Runner) Started() <-chan struct{} {
	return r.started
}

// WaitStatus returns the process wait status
func (r *Runner) WaitStatus() process.WaitStatus {
	return &waitStatus{exitCode: r.exitCode}
}

// Interrupt sends an interrupt signal to the container
func (r *Runner) Interrupt() error {
	return r.Signal(process.SIGINT)
}

// Terminate sends a terminate signal to the container
func (r *Runner) Terminate() error {
	return r.Signal(process.SIGTERM)
}

// Run starts the container and waits for it to complete
func (r *Runner) Run(ctx context.Context) error {
	if err := r.Start(ctx); err != nil {
		return err
	}
	return r.Wait()
}

// Signal sends a signal to the container process
func (r *Runner) Signal(sig process.Signal) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.containerID == "" {
		return fmt.Errorf("container not started")
	}

	// Convert signal to string for docker kill
	var sigStr string
	switch sig {
	case process.SIGTERM:
		sigStr = "TERM"
	case process.SIGINT:
		sigStr = "INT"
	default:
		sigStr = "TERM"
	}

	r.logger.Debug("Sending signal %s to container: %s", sigStr, r.containerID)

	cmd := exec.Command("docker", "kill", "--signal", sigStr, r.containerID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to send signal to container: %w", err)
	}

	return nil
}

// IsDockerAvailable checks if Docker is available on the system
func IsDockerAvailable() bool {
	cmd := exec.Command("docker", "--version")
	return cmd.Run() == nil
}
