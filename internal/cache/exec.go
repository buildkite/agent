package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache/archive"
	"github.com/buildkite/agent/v4/internal/cache/configuration"
	"github.com/buildkite/agent/v4/logger"
	"go.opentelemetry.io/otel"
)

// outputTargetPath is added to target_paths to address cache exec's entries, which hold the files and the recorded output, so they never replace or get replaced by plain cache save's; "<...>" can't be a real file path and stands out in the registry's entry list.
const outputTargetPath = "<cache-exec-output>"

// errUnreadableOutput means an entry's recorded output is missing or unreadable, so it can't be a hit.
var errUnreadableOutput = errors.New("recorded command output is missing or unreadable")

// Command runs the wrapped command with the given streams; a non-nil error means it failed and is returned by RunExec.
type Command func(stdout, stderr io.Writer) error

// RunExec runs command unless the cache cfg.Names[0] holds an exact-key result, in which case it restores target_paths and replays the recorded output; on success after a miss it saves both as one entry, and cache failures follow cfg.FailOnError.
func RunExec(ctx context.Context, l logger.Logger, apiClient *api.Client, cfg Config, stdout, stderr io.Writer, command Command) error {
	c, _, err := newClient(l, apiClient, cfg)
	if err == nil && c == nil {
		err = fmt.Errorf("cache names not found in configuration: %s", cfg.Names[0])
	}
	if err != nil {
		return RunUncached(l, cfg.FailOnError, err, stdout, stderr, command)
	}
	c.onProgress = func(cacheID, stage, message string, _, _ int) {
		l.WithFields(
			logger.StringField("cache_id", cacheID),
			logger.StringField("stage", stage),
			logger.StringField("message", message),
		).Debugf("Cache progress")
	}
	return c.exec(ctx, l, cfg.Names[0], cfg.FailOnError, cfg.Redact, stdout, stderr, command)
}

// RunUncached runs command without the cache because of err, a cache problem, or returns err if failOnError.
func RunUncached(l logger.Logger, failOnError bool, err error, stdout, stderr io.Writer, command Command) error {
	if failOnError {
		return err
	}
	l.Warnf("%v; running the command without caching", err)
	return command(stdout, stderr)
}

func (c *client) exec(ctx context.Context, l logger.Logger, cacheID string, failOnError bool, redact Redactor, stdout, stderr io.Writer, command Command) error {
	ctx, span := otel.Tracer("github.com/buildkite/agent/v4/internal/cache").Start(ctx, "Client.exec")
	defer span.End()
	start := time.Now()

	cacheConfig, err := c.findCache(cacheID)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(cacheConfig.CacheKey, func(p configuration.KeyPart) bool { return p.FallbackLimit }) {
		l.Infof("cache exec ignores fallback_limit for %q: only an exact cache_key match skips the command", cacheID)
	}

	s := newStreams(stdout, stderr)

	// A cache problem before anything was restored is a miss: run uncached unless failOnError.
	runUncached := func(err error) error {
		return RunUncached(l, failOnError, err, s.Stdout(), s.Stderr(), command)
	}

	// Resolve the key before the command can change its inputs, with every part mandatory: replaying a fallback's output would misrepresent what ran.
	cacheKey, err := c.resolveCacheKey(cacheConfig)
	if err != nil {
		return runUncached(fmt.Errorf("failed to resolve cache key for %q: %w", cacheID, err))
	}
	for i := range cacheKey {
		cacheKey[i].Mandatory = true
	}

	s.header("--- :package: Restoring cache...")
	hit, err := c.execRestore(ctx, l, cacheConfig, cacheKey, failOnError, s, start)
	if err != nil || hit {
		return err
	}

	rec, err := newOutputRecorder()
	if err != nil {
		return runUncached(err)
	}
	defer func() { _ = os.Remove(rec.Path()) }()

	s.header("+++ :package: No cached result, running command")
	commandStart := time.Now()
	runErr := command(io.MultiWriter(s.Stdout(), rec.Stdout()), io.MultiWriter(s.Stderr(), rec.Stderr()))
	outputErr := rec.Close(time.Since(commandStart))
	if runErr != nil {
		return runErr
	}
	s.header("--- :package: Saving cache...")

	// Redact once, after the command, so secrets it registered while running (e.g. with secret get) are caught; the raw recording stays in a temp file the job can already read.
	var output string
	if outputErr == nil {
		output, outputErr = redactRecording(ctx, rec.Path(), redact)
		if output != "" {
			defer func() { _ = os.Remove(output) }()
		}
	}
	// Remove the raw recording before saving: if the temp dir is under a target path, it would be archived with its secrets.
	if err := os.Remove(rec.Path()); err != nil && outputErr == nil {
		outputErr = fmt.Errorf("failed to remove the unredacted output recording: %w", err)
	}
	return c.execSave(ctx, l, cacheConfig.Name, cacheKey, failOnError, output, outputErr)
}

// execRestore reports a hit, restoring target_paths and replaying the recorded output, only for an exact match of cache exec's entry; otherwise the command runs.
func (c *client) execRestore(ctx context.Context, l logger.Logger, cacheConfig *configuration.Cache, cacheKey []api.CacheKeyPart, failOnError bool, s *streams, start time.Time) (bool, error) {
	cacheID := cacheConfig.Name

	output, err := os.CreateTemp("", "cache-exec-output-*.zst")
	if err != nil {
		err = fmt.Errorf("failed to restore cache %q: %w", cacheID, err)
		if failOnError {
			return false, err
		}
		l.Warnf("%v; running the command", err)
		return false, nil
	}
	_ = output.Close()
	defer func() { _ = os.Remove(output.Name()) }()

	// Take the recording out of the archive, never into the workspace, and check it before restore touches target_paths.
	var ranFor time.Duration // how long the command ran when its output was recorded
	result, err := c.restore(ctx, cacheID, cacheKey, func(archiveFile string) error {
		if err := archive.ExtractCommandOutput(archiveFile, output.Name()); err != nil {
			if errors.Is(err, archive.ErrNoCommandOutput) {
				return fmt.Errorf("%w: %w", errUnreadableOutput, err)
			}
			return err
		}
		var err error
		if ranFor, err = replayOutput(output.Name(), io.Discard, io.Discard); err != nil {
			return fmt.Errorf("%w: %w", errUnreadableOutput, err)
		}
		return nil
	})
	if err != nil {
		// As in restore, fail rather than run the command against half-restored target paths.
		if failOnError || errors.Is(err, errRestoreMutatedTargets) {
			l.Warnf("%s", restoreReport(cacheID, result, err))
			return false, fmt.Errorf("failed to restore cache %q: %w", cacheID, err)
		}
		l.Warnf("%s; running the command", restoreReport(cacheID, result, err))
		return false, nil
	}
	l.Infof("%s", restoreReport(cacheID, result, nil))
	if !result.CacheRestored || !result.CacheHit {
		return false, nil
	}

	s.header(replayHeader(ranFor - time.Since(start)))
	if _, err := replayOutput(output.Name(), s.Stdout(), s.Stderr()); err != nil {
		// The files are restored and the output was readable before, so only writing it failed; that isn't a reason to fail the build.
		err = fmt.Errorf("failed to replay cached output for %q: %w", cacheID, err)
		if failOnError {
			return true, err
		}
		l.Warnf("%v", err)
		return true, nil
	}
	s.header(timeSaved(ranFor, time.Since(start)))
	return true, nil
}

// replayHeader opens the replayed output's log group, highlighting in its title the time the hit is saving (Buildkite renders ANSI colours in group titles).
func replayHeader(saved time.Duration) string {
	if saved <= 0 {
		return "+++ :package: Replaying output from cache (command was not run)"
	}
	return fmt.Sprintf("+++ ⚡ \x1b[1;32mcache exec saved %s\x1b[0m", roundDuration(saved))
}

// timeSaved summarises a hit, warning in yellow when restoring took longer than running the command.
func timeSaved(ranFor, took time.Duration) string {
	if ranFor > 0 && took >= ranFor {
		return fmt.Sprintf("\x1b[33m⚠\x1b[0m Restored from cache in \x1b[1m%s\x1b[0m, but running the command took only \x1b[1m%s\x1b[0m: caching it isn't saving time",
			roundDuration(took), roundDuration(ranFor))
	}
	return "\x1b[32m✔\x1b[0m Restored from cache"
}

// roundDuration rounds to milliseconds under a second, tenths of a second under a minute, and seconds above.
func roundDuration(d time.Duration) time.Duration {
	if d < time.Second {
		return d.Round(time.Millisecond)
	}
	if d < time.Minute {
		return d.Round(100 * time.Millisecond)
	}
	return d.Round(time.Second)
}

// execSave saves the target paths and the recorded output as one entry, or nothing if the output can't be saved.
func (c *client) execSave(ctx context.Context, l logger.Logger, cacheID string, cacheKey []api.CacheKeyPart, failOnError bool, output string, outputErr error) error {
	failed := func(what string, err error) error {
		if failOnError {
			return fmt.Errorf("failed to save %s %q: %w", what, cacheID, err)
		}
		l.WithFields(
			logger.StringField("cache_id", cacheID),
			logger.StringField("error", err.Error()),
		).Warnf("Failed to save %s; continuing without failing the build", what)
		return nil
	}

	if outputErr != nil {
		return failed("cache (command output couldn't be saved)", outputErr)
	}
	l.Infof("Saving cache: %s", cacheID)
	result, err := c.save(ctx, cacheID, cacheKey, output)
	if err != nil {
		return failed("cache", err)
	}
	logSaveResult(l, cacheID, result)
	return nil
}

// outputTargetPaths returns the target_paths addressing cache exec's entry for a cache.
func outputTargetPaths(targetPaths []string) []string {
	return append(slices.Clone(targetPaths), outputTargetPath)
}

// streams carries the command's output and cache exec's section headers. Headers go to stderr, so stdout holds only the command's output, and each starts on a new line even if the output didn't end with one.
type streams struct {
	stdout, stderr io.Writer
	mu             sync.Mutex
	midLine        bool // the last byte written to either stream wasn't a newline
}

func newStreams(stdout, stderr io.Writer) *streams {
	return &streams{stdout: stdout, stderr: stderr}
}

func (s *streams) Stdout() io.Writer { return lineTracker{s, s.stdout} }
func (s *streams) Stderr() io.Writer { return lineTracker{s, s.stderr} }

func (s *streams) header(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.midLine {
		text = "\n" + text
	}
	_, _ = fmt.Fprintln(s.stderr, text)
	s.midLine = false
}

type lineTracker struct {
	s *streams
	w io.Writer
}

func (t lineTracker) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > 0 {
		t.s.mu.Lock()
		t.s.midLine = p[n-1] != '\n'
		t.s.mu.Unlock()
	}
	return n, err
}
