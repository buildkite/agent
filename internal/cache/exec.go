package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache/configuration"
	"github.com/buildkite/agent/v4/logger"
	"github.com/dustin/go-humanize"
)

// Command runs the wrapped command with the given output streams; a non-nil error means it failed.
type Command func(stdout, stderr io.Writer) error

// Redactor returns the command's output with the job's secrets redacted.
type Redactor func(ctx context.Context, output []byte) ([]byte, error)

// maxOutput caps the command output cache exec saves; a var so tests can lower it.
var maxOutput = 10 << 20

// RunExec runs command unless the cache cfg.Names[0] has an entry for the current cache key, in which case it
// restores target_paths and replays the command's recorded output instead. After a successful run it saves
// target_paths and the output as one entry. Cache failures follow cfg.FailOnError, as with save and restore.
//
// The output is saved as a log file added to the cache's target_paths, so the existing save and restore do all
// the work. Since an entry's address is its cache_key and target_paths, the extra path also keeps exec's entries
// apart from plain cache save's.
func RunExec(ctx context.Context, l logger.Logger, apiClient *api.Client, cfg Config, stdout, stderr io.Writer, command Command) error {
	start := time.Now()
	name := cfg.Names[0]

	c, _, err := newClient(l, apiClient, cfg)
	if err == nil && c == nil {
		err = fmt.Errorf("cache names not found in configuration: %s", name)
	}
	if err != nil {
		return RunUncached(l, cfg.FailOnError, err, stdout, stderr, command)
	}
	c.onProgress = nil
	cacheConfig, _ := c.findCache(name) // newClient checked it exists

	// Resolve the key before the command can change its inputs, and pin it as literal parts so the save uses the
	// same key. Literal parts are all mandatory: replaying a fallback's output would misrepresent what ran.
	key, err := c.resolveCacheKey(cacheConfig)
	if err != nil {
		return RunUncached(l, cfg.FailOnError, fmt.Errorf("failed to resolve cache key for %q: %w", name, err), stdout, stderr, command)
	}
	if slices.ContainsFunc(cacheConfig.CacheKey, func(p configuration.KeyPart) bool { return p.FallbackLimit }) {
		l.Infof("cache exec ignores fallback_limit for %q: only an exact cache_key match skips the command", name)
	}
	cacheConfig.CacheKey = make([]configuration.KeyPart, len(key))
	for i, part := range key {
		cacheConfig.CacheKey[i] = configuration.KeyPart{Source: configuration.SourceLiteral, Arg: part.Value}
	}

	// The format version is in the name, which is part of the entry's address, so a new format never reads old entries.
	logPath := ".buildkite-cache-exec-" + name + ".v1.log"
	cacheConfig.TargetPaths = append(slices.Clone(cacheConfig.TargetPaths), logPath)
	defer func() { _ = os.Remove(logPath) }()

	// Headers go to stderr, so stdout holds only the command's output.
	_, _ = fmt.Fprintln(stderr, "--- :package: Restoring cache")
	result, err := c.Restore(ctx, name)
	switch {
	case err == nil:
		l.Infof("%s", restoreReport(name, result, nil))
		if result.CacheRestored && replay(logPath, time.Since(start), stdout, stderr) {
			return nil
		}
	case cfg.FailOnError || errors.Is(err, errRestoreMutatedTargets):
		// Don't run the command against half-restored target paths.
		l.Warnf("%s", restoreReport(name, result, err))
		return fmt.Errorf("failed to restore cache %q: %w", name, err)
	default:
		l.Warnf("%s; running the command", restoreReport(name, result, err))
	}

	_, _ = fmt.Fprintln(stderr, "+++ :package: No cached result, running command")
	rec := &recorder{}
	commandStart := time.Now()
	if err := command(io.MultiWriter(stdout, rec), io.MultiWriter(stderr, rec)); err != nil {
		return err
	}
	ranFor := time.Since(commandStart)

	if rec.midLine {
		_, _ = fmt.Fprintln(stderr)
	}
	_, _ = fmt.Fprintln(stderr, "--- :package: Saving cache")
	err = writeLog(ctx, cfg.Redact, logPath, rec, ranFor)
	if err == nil {
		l.Infof("Saving cache: %s", name)
		var saved SaveResult
		if saved, err = c.Save(ctx, name); err == nil && saved.CacheEntryCreated {
			l.Infof("Cache saved: %s", name)
		} else if err == nil {
			l.Infof("Cache already exists, not saving: %s", name)
		}
	}
	if err != nil {
		if cfg.FailOnError {
			return fmt.Errorf("failed to save cache %q: %w", name, err)
		}
		l.Warnf("Failed to save cache %q: %v; continuing without failing the build", name, err)
	}
	return nil
}

// RunUncached runs command without the cache because of err, a cache problem, or returns err if failOnError.
func RunUncached(l logger.Logger, failOnError bool, err error, stdout, stderr io.Writer, command Command) error {
	if failOnError {
		return err
	}
	l.Warnf("%v; running the command without caching", err)
	return command(stdout, stderr)
}

// writeLog writes the redacted output to path, after a first line holding how long the command ran.
func writeLog(ctx context.Context, redact Redactor, path string, rec *recorder, ranFor time.Duration) error {
	if rec.tooLarge {
		return fmt.Errorf("command output is larger than %s", humanize.IBytes(uint64(maxOutput)))
	}
	// Redact after the command, so secrets it registered while running (e.g. with secret get) are caught.
	output, err := redact(ctx, rec.buf.Bytes())
	if err != nil {
		return fmt.Errorf("failed to redact secrets from the command output: %w", err)
	}
	return os.WriteFile(path, append([]byte(ranFor.String()+"\n"), output...), 0o600)
}

// replay writes the restored log at path to stdout, under a header showing the time saved, and reports whether
// the log was readable.
func replay(path string, restoreTook time.Duration, stdout, stderr io.Writer) bool {
	log, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	first, output, _ := bytes.Cut(log, []byte("\n"))
	ranFor, err := time.ParseDuration(string(first))
	if err != nil {
		return false
	}
	_, _ = fmt.Fprintln(stderr, replayHeader(ranFor-restoreTook))
	_, _ = stdout.Write(output)
	return true
}

// replayHeader opens the replayed output's log group, making clear the command didn't run and highlighting the
// time saved (Buildkite renders ANSI colours in group titles).
func replayHeader(saved time.Duration) string {
	if saved < time.Second {
		return "+++ :package: Cache hit: replaying output, the command was not run"
	}
	return fmt.Sprintf("+++ ⚡ \x1b[1;32mCache hit saved %s\x1b[0m: replaying output, the command was not run", saved.Round(time.Second))
}

// recorder keeps the command's combined output, up to maxOutput, and whether it ended mid-line.
type recorder struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	tooLarge bool
	midLine  bool
}

func (r *recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(p) > 0 {
		r.midLine = p[len(p)-1] != '\n'
	}
	r.tooLarge = r.tooLarge || r.buf.Len()+len(p) > maxOutput
	if !r.tooLarge {
		r.buf.Write(p)
	}
	return len(p), nil
}
