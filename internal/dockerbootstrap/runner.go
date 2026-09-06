package dockerbootstrap

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const SetupFailure = 125

// Runner gives cancellation a grace period and reserves a separate cleanup budget.
type Runner struct {
	Client Clienter
	Stdout io.Writer
	Stderr io.Writer
}

func (r Runner) Run(ctx context.Context, cfg Config) (code int, err error) {
	job, err := prepare(cfg)
	if err != nil {
		return SetupFailure, err
	}
	if r.Stdout == nil {
		r.Stdout = io.Discard
	}
	if r.Stderr == nil {
		r.Stderr = io.Discard
	}
	r.Client = withDiagnostics(r.Client, cfg.Environment)
	// Cleanup and client shutdown must finish before JobRunner's SIGKILL deadline.
	// Reserve 10% of the cleanup margin for scheduling delays.
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()
	go func() {
		select {
		case <-ctx.Done():
		case <-shutdownCtx.Done():
			return
		}
		timer := time.NewTimer(job.grace + cfg.CleanupMargin - cfg.CleanupMargin/10)
		defer timer.Stop()
		select {
		case <-timer.C:
			shutdownCancel()
		case <-shutdownCtx.Done():
		}
	}()
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return SetupFailure, err
	}
	name := "buildkite-bootstrap-" + hex.EncodeToString(suffix[:])
	operation := func(parent context.Context, timeout time.Duration, args ...string) error {
		callCtx, cancel := context.WithTimeout(parent, timeout)
		defer cancel()
		_, err := r.Client.Run(callCtx, args, nil, io.Discard, io.Discard)
		return err
	}
	if err := operation(ctx, cfg.OperationTimeout, "info"); err != nil {
		return SetupFailure, err
	}
	if err := operation(ctx, cfg.OperationTimeout, "image", "inspect", cfg.Image); err != nil {
		if ctx.Err() != nil {
			return SetupFailure, ctx.Err()
		}
		if err := operation(ctx, cfg.PullTimeout, "pull", cfg.Image); err != nil {
			return SetupFailure, err
		}
	}
	// Full inspection output could disclose image environment secrets in job logs.
	var identity bytes.Buffer
	inspectCtx, inspectCancel := context.WithTimeout(ctx, cfg.OperationTimeout)
	_, inspectErr := r.Client.Run(inspectCtx, []string{"image", "inspect", "--format", "{{.Id}} {{json .RepoDigests}}", cfg.Image}, nil, &identity, io.Discard)
	inspectCancel()
	if inspectErr != nil {
		return SetupFailure, inspectErr
	}
	fields := strings.Fields(identity.String())
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "sha256:") {
		return SetupFailure, fmt.Errorf("docker image inspection returned no image ID")
	}
	imageID := fields[0]
	_, _ = fmt.Fprintf(r.Stdout, "Docker bootstrap image: %s (%s)\n", cfg.Image, strings.TrimSpace(identity.String()))

	// Docker may create the container but lose the response; its name still
	// allows cleanup after an apparently failed create.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(shutdownCtx, cfg.CleanupMargin)
		defer cancel()
		status, removeErr := r.Client.Run(cleanupCtx, []string{"rm", "--force", name}, nil, io.Discard, io.Discard)
		if removeErr == nil && status == 0 {
			return
		}
		// A failed remove can mean either auto-removal or daemon loss.
		var found bytes.Buffer
		status, listErr := r.Client.Run(cleanupCtx, []string{"ps", "--all", "--quiet", "--filter", "name=^/" + name + "$"}, nil, &found, io.Discard)
		if listErr == nil && status == 0 && strings.TrimSpace(found.String()) == "" {
			return
		}
		_, _ = fmt.Fprintf(r.Stderr, "Docker bootstrap cleanup failed; container may remain: %s: %v\n", name, removeErr)
		if listErr != nil {
			_, _ = fmt.Fprintf(r.Stderr, "Could not verify container removal: %v\n", listErr)
		}
		if err == nil && code == 0 {
			code, err = SetupFailure, fmt.Errorf("docker container cleanup failed: %w", removeErr)
		}
	}()
	args := append([]string{"create", "--pull", "never", "--name", name, "--label", "com.buildkite.bootstrap=docker", "--label", "com.buildkite.agent=true"}, job.args...)
	for _, label := range []struct{ key, variable string }{
		{"com.buildkite.job-id", "BUILDKITE_JOB_ID"},
		{"com.buildkite.agent-id", "BUILDKITE_AGENT_ID"},
		{"com.buildkite.agent-name", "BUILDKITE_AGENT_NAME"},
	} {
		if value := job.env[label.variable]; value != "" {
			args = append(args, "--label", label.key+"="+value)
		}
	}
	// The mounted agent must take precedence over image binaries. exec lets
	// Docker's init deliver signals directly to bootstrap.
	const script = `set -e
export PATH="/buildkite-docker/bin:$PATH"
if ! test -x /buildkite-docker/bin/buildkite-agent || ! command -v git >/dev/null || ! command -v ssh >/dev/null; then
  echo 'Docker bootstrap image contract requires an executable agent binary, git, and ssh' >&2
  exit 125
fi
if ! /buildkite-docker/bin/buildkite-agent --version >/dev/null; then
  echo 'Docker bootstrap image contract: mounted agent binary cannot run' >&2
  exit 125
fi
exec /buildkite-docker/bin/buildkite-agent bootstrap`
	// A concurrent tag refresh must not change the image after inspection.
	args = append(args, imageID, "-c", script)
	createCtx, createCancel := context.WithTimeout(ctx, cfg.OperationTimeout)
	// Docker create reads console dimensions from stdout; capturing the ID
	// through a pipe would lose the supervisor's PTY dimensions.
	_, createErr := r.Client.Run(createCtx, args, job.env, r.Stdout, io.Discard)
	createCancel()
	if createErr != nil {
		return SetupFailure, createErr
	}
	if ctx.Err() != nil {
		return SetupFailure, ctx.Err()
	}

	// start --attach registers the exit observer before starting the container.
	// Separate attach/wait processes can lose fast exits with --rm.
	attachCtx, attachCancel := context.WithCancel(context.Background())
	defer attachCancel()
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		code, err := r.Client.Run(attachCtx, []string{"start", "--attach", name}, nil, r.Stdout, r.Stderr)
		done <- result{code, err}
	}()
	// The operation timeout must not limit a running job's duration.
	startup := time.NewTimer(cfg.OperationTimeout)
	defer startup.Stop()
	startupDeadline := startup.C
	draining := false
	var drainProbeErr error
waiting:
	for {
		select {
		case result := <-done:
			if result.err != nil || result.code < 0 {
				return SetupFailure, fmt.Errorf("docker attachment failed")
			}
			return r.completed(shutdownCtx, cfg.CleanupMargin/4, name, result.code)
		case <-ctx.Done():
			break waiting
		case <-startupDeadline:
			if draining {
				attachCancel()
				<-done
				if drainProbeErr != nil {
					return SetupFailure, fmt.Errorf("docker attachment did not finish within the drain timeout: %w", drainProbeErr)
				}
				return SetupFailure, fmt.Errorf("docker attachment did not finish within the drain timeout")
			}
			var state bytes.Buffer
			probeCtx, probeCancel := context.WithTimeout(ctx, cfg.OperationTimeout)
			_, probeErr := r.Client.Run(probeCtx, []string{"inspect", "--format", "{{.State.Status}}", name}, nil, &state, io.Discard)
			probeCancel()
			if ctx.Err() != nil {
				break waiting
			}
			if probeErr == nil && strings.TrimSpace(state.String()) == "created" {
				// Inspection may be stale by the time attachment completes.
				select {
				case result := <-done:
					if result.err != nil || result.code < 0 {
						return SetupFailure, fmt.Errorf("docker attachment failed")
					}
					return r.completed(shutdownCtx, cfg.CleanupMargin/4, name, result.code)
				default:
				}
				attachCancel()
				<-done
				return SetupFailure, fmt.Errorf("docker container did not start within the operation timeout")
			}
			if status := strings.TrimSpace(state.String()); probeErr == nil && (status == "running" || status == "paused") {
				startupDeadline = nil
				continue
			}
			// Logs and exit status may still be draining after exit or auto-removal,
			// including when inspection fails because the container is already gone.
			draining, drainProbeErr = true, probeErr
			startup.Reset(cfg.OperationTimeout)
			startupDeadline = startup.C
		}
	}

	// Docker cannot signal a container that has not started yet.
	graceCtx, graceCancel := context.WithTimeout(context.Background(), job.grace)
	defer graceCancel()
	signal := job.env["BUILDKITE_CANCEL_SIGNAL"]
	if signal == "" || signal == "SIGKILL" {
		signal = "SIGTERM"
	}
	retryDelay := 50 * time.Millisecond
	for {
		if err := operation(graceCtx, cfg.OperationTimeout, "kill", "--signal", signal, name); err == nil {
			break
		}
		select {
		case result := <-done:
			if result.err != nil || result.code < 0 {
				return SetupFailure, fmt.Errorf("docker attachment failed during cancellation")
			}
			return r.completed(shutdownCtx, cfg.CleanupMargin/4, name, result.code)
		case <-graceCtx.Done():
			goto force
		case <-time.After(retryDelay):
			retryDelay = min(2*retryDelay, time.Second)
		}
	}
	select {
	case result := <-done:
		if result.err != nil || result.code < 0 {
			return SetupFailure, fmt.Errorf("docker attachment failed during cancellation")
		}
		return r.completed(shutdownCtx, cfg.CleanupMargin/4, name, result.code)
	case <-graceCtx.Done():
	}
force:
	_, _ = fmt.Fprintln(r.Stderr, "Docker bootstrap cancellation grace expired; forcing container removal")
	// The deferred force-remove kills the container; the separate CLI process
	// also needs cancellation to avoid outliving the supervisor.
	attachCancel()
	<-done
	return 137, nil
}

// Auto-removal can erase the evidence needed to distinguish a Docker failure
// from a job exit; preserve the CLI status when inspection is unavailable.
func (r Runner) completed(ctx context.Context, timeout time.Duration, name string, code int) (int, error) {
	if code == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var out bytes.Buffer
	status, err := r.Client.Run(ctx, []string{"inspect", "--format", "{{json .State}}", name}, nil, &out, io.Discard)
	var state struct {
		Running   bool
		Status    string
		OOMKilled bool
		Error     string
	}
	if err == nil && status == 0 && json.Unmarshal(out.Bytes(), &state) == nil {
		if state.OOMKilled {
			_, _ = fmt.Fprintln(r.Stderr, "Docker bootstrap container was OOM-killed")
		}
		if state.Running || state.Status == "created" || state.Error != "" {
			return SetupFailure, fmt.Errorf("docker container failed to start or attachment ended while still running")
		}
	}
	return code, nil
}
