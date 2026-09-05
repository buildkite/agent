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

// Runner owns one container, including cleanup after partially completed create
// operations. Cancellation of ctx initiates graceful container cancellation;
// cleanup uses a separate bounded context.
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
	// All cancellation cleanup shares one outer budget, including time spent
	// waiting for a killed Docker client. Leave a little scheduling headroom
	// before JobRunner SIGKILLs this supervisor.
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
		status, err := r.Client.Run(callCtx, args, nil, io.Discard, io.Discard)
		if err != nil {
			return fmt.Errorf("docker %s failed: %w", args[0], err)
		}
		if status != 0 {
			return fmt.Errorf("docker %s failed (exit %d)", args[0], status)
		}
		return nil
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
	// Inspect only safe image identity fields, not image configuration or env.
	var identity bytes.Buffer
	inspectCtx, inspectCancel := context.WithTimeout(ctx, cfg.OperationTimeout)
	status, inspectErr := r.Client.Run(inspectCtx, []string{"image", "inspect", "--format", "{{.Id}} {{json .RepoDigests}}", cfg.Image}, nil, &identity, io.Discard)
	inspectCancel()
	if inspectErr != nil || status != 0 {
		return SetupFailure, fmt.Errorf("docker image identity inspection failed")
	}
	fields := strings.Fields(identity.String())
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "sha256:") {
		return SetupFailure, fmt.Errorf("docker image inspection returned no image ID")
	}
	imageID := fields[0]
	_, _ = fmt.Fprintf(r.Stdout, "Docker bootstrap image: %s (%s)\n", cfg.Image, strings.TrimSpace(identity.String()))

	// Register cleanup before create: Docker may create the container but lose
	// the response, leaving us with only its preallocated unique name.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(shutdownCtx, cfg.CleanupMargin)
		defer cancel()
		status, removeErr := r.Client.Run(cleanupCtx, []string{"rm", "--force", name}, nil, io.Discard, io.Discard)
		if removeErr == nil && status == 0 {
			return
		}
		// Auto-remove commonly wins the race. A successful list with no matching
		// resource distinguishes an already-removed container from daemon loss.
		var found bytes.Buffer
		status, listErr := r.Client.Run(cleanupCtx, []string{"ps", "--all", "--quiet", "--filter", "name=^/" + name + "$"}, nil, &found, io.Discard)
		if listErr == nil && status == 0 && strings.TrimSpace(found.String()) == "" {
			return
		}
		_, _ = fmt.Fprintln(r.Stderr, "Docker bootstrap cleanup failed; container may remain:", name)
		if err == nil && code == 0 {
			code, err = SetupFailure, fmt.Errorf("docker container cleanup failed")
		}
	}()
	args := append([]string{"create", "--pull", "never", "--name", name, "--label", "com.buildkite.bootstrap=docker", "--label", "com.buildkite.agent=true"}, job.args...)
	// Override the image entrypoint, validate the minimum image contract, and
	// exec bootstrap so Docker's init forwards signals directly to it.
	const script = `set -e
if ! test -x /buildkite-docker/bin/buildkite-agent || ! command -v git >/dev/null || ! command -v ssh >/dev/null; then
  echo 'Docker bootstrap image contract requires an executable agent binary, git, and ssh' >&2
  exit 125
fi
if ! /buildkite-docker/bin/buildkite-agent --version >/dev/null; then
  echo 'Docker bootstrap image contract: mounted agent binary cannot run' >&2
  exit 125
fi
exec /buildkite-docker/bin/buildkite-agent bootstrap`
	// Use the inspected immutable ID so a concurrent tag update cannot change
	// which image runs after we have logged its identity.
	args = append(args, imageID, "-c", script)
	createCtx, createCancel := context.WithTimeout(ctx, cfg.OperationTimeout)
	// Keep stdout connected to the supervisor PTY so Docker records its initial
	// console size. The only create output is the new container ID.
	status, createErr := r.Client.Run(createCtx, args, job.env, r.Stdout, io.Discard)
	createCancel()
	if createErr != nil || status != 0 {
		return SetupFailure, fmt.Errorf("docker container create failed")
	}
	if ctx.Err() != nil {
		return SetupFailure, ctx.Err()
	}

	// Docker start --attach performs attach, wait registration, and start in
	// that order. Launching separate attach/wait CLI processes cannot establish
	// that ordering and loses fast exits when --rm is enabled.
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
	// Bound start/attach without imposing a wall-clock limit on a running job.
	startup := time.NewTimer(cfg.OperationTimeout)
	defer startup.Stop()
	startupDeadline := startup.C
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
			var state bytes.Buffer
			probeCtx, probeCancel := context.WithTimeout(ctx, cfg.OperationTimeout)
			status, probeErr := r.Client.Run(probeCtx, []string{"inspect", "--format", "{{.State.Running}}", name}, nil, &state, io.Discard)
			probeCancel()
			if ctx.Err() != nil {
				break waiting
			}
			if probeErr != nil || status != 0 || strings.TrimSpace(state.String()) != "true" {
				// Prefer a concurrently completed attachment, including fast exits.
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
			startupDeadline = nil
		}
	}

	// Signal delivery may race with container startup. Retry until Docker
	// accepts the signal, attachment finishes, or the inner grace expires.
	graceCtx, graceCancel := context.WithTimeout(context.Background(), job.grace)
	defer graceCancel()
	signal := job.env["BUILDKITE_CANCEL_SIGNAL"]
	if signal == "" || signal == "SIGKILL" {
		signal = "SIGTERM"
	}
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
		case <-time.After(50 * time.Millisecond):
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
	// Deferred force-remove kills the container as well as removing it. Stop
	// the attached CLI now so it cannot outlive the supervisor.
	attachCancel()
	<-done
	return 137, nil
}

// completed distinguishes known runtime/client failures from bootstrap exits.
// Auto-remove can win the race, so a missing state preserves the CLI exit code.
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
