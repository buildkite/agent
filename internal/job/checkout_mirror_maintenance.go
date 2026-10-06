package job

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/dustin/go-humanize"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// staleGitMaintenanceFiles are the files, relative to a bare repository, that
// a `git gc` or `git maintenance run` killed part-way through leaves behind.
// Git only cleans these up itself much later, if ever:
//
//   - gc.pid: `git gc --auto` seeing one written by another hostname assumes
//     that gc is still running and does nothing for 12 hours.
//   - gc.log.lock: the lock a detached gc holds on gc.log; a detached gc dies
//     at startup while it exists. Git 2.47 moved detaching from gc to
//     `git maintenance`, so only older Git creates it and is blocked by it.
//   - objects/maintenance.lock: `git maintenance run --auto` (which every
//     `git fetch` runs) silently does nothing while it exists.
var staleGitMaintenanceFiles = []string{
	"gc.pid",
	"gc.log.lock",
	filepath.Join("objects", "maintenance.lock"),
}

// isStaleGitPackFile reports whether a file name in objects/pack is a
// temporary file that Git writes while repacking and renames into place only
// once complete:
//
//   - tmp_pack_*, tmp_idx_*, tmp_rev_*, tmp_mtimes_* and tmp_bitmap_* from
//     pack-objects and index-pack. `git prune` removes these too, but only
//     when they are older than gc.pruneExpire (2 weeks by default).
//   - .tmp-<pid>-pack-<hash>.{pack,idx,rev,bitmap,mtimes,promisor} from
//     `git repack`, which stages a finished pack under this name before
//     renaming it. Nothing in Git ever removes these.
//
// A partial pack can be gigabytes, so on a cache volume that is copied
// forward each job, every interrupted gc grows it by that much.
func isStaleGitPackFile(name string) bool {
	return strings.HasPrefix(name, "tmp_") || repackStagingFile.MatchString(name)
}

// repackStagingFile matches the ".tmp-<pid>-pack" prefix that `git repack`
// gives packs it has written but not yet renamed into place.
var repackStagingFile = regexp.MustCompile(`^\.tmp-[0-9]+-pack-`)

// removeStaleGitMaintenanceFiles removes the leftovers of an interrupted git
// gc / git maintenance from the bare repository at mirrorDir. It returns the
// paths removed (relative to mirrorDir) and their total size. A failure to
// remove one file is reported in err but does not stop the others from being
// removed.
//
// There is deliberately no check for whether the Git process that created a
// file is still running: a running gc writes to exactly these files, so this
// is only safe to call when nothing else can be running Git against the
// mirror. The agent's mirror update lock does not provide that guarantee on
// its own, because gc detaches from the fetch that started it and keeps going
// after the lock is released. That includes a fetch earlier in the same job,
// which is why the executor only calls this on its first visit to a mirror.
func removeStaleGitMaintenanceFiles(mirrorDir string) (removed []string, totalBytes int64, err error) {
	candidates := slices.Clone(staleGitMaintenanceFiles)

	packDir := filepath.Join("objects", "pack")
	entries, readErr := os.ReadDir(filepath.Join(mirrorDir, packDir))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		err = errors.Join(err, fmt.Errorf("listing %s: %w", packDir, readErr))
	}
	for _, entry := range entries {
		if isStaleGitPackFile(entry.Name()) {
			candidates = append(candidates, filepath.Join(packDir, entry.Name()))
		}
	}

	for _, rel := range candidates {
		path := filepath.Join(mirrorDir, rel)
		info, statErr := os.Lstat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			err = errors.Join(err, fmt.Errorf("checking %s: %w", rel, statErr))
			continue
		}
		if removeErr := removeFile(path, info); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("removing %s: %w", rel, removeErr))
			continue
		}
		removed = append(removed, rel)
		if info.Mode().IsRegular() {
			totalBytes += info.Size()
		}
	}
	return removed, totalBytes, err
}

// removeFile removes path. Git marks a finished pack read-only before renaming
// it into place, and on Windows a read-only file cannot be removed, so a
// permission failure on a read-only regular file is retried after making it
// writable, like Git's own unlink wrapper does.
func removeFile(path string, info os.FileInfo) error {
	err := os.Remove(path)
	if err == nil || !errors.Is(err, os.ErrPermission) || !info.Mode().IsRegular() || info.Mode().Perm()&0o200 != 0 {
		return err
	}
	if chmodErr := os.Chmod(path, info.Mode().Perm()|0o200); chmodErr != nil {
		return err
	}
	return os.Remove(path)
}

// markMirrorVisited records that this job is about to clone or fetch into
// mirrorDir and reports whether this is the first time. It must be called
// before any Git command runs against the mirror, so a later visit in the
// same job (a checkout retry, a submodule URL that repeats) never treats a
// gc started by the earlier visit as stale.
func (e *Executor) markMirrorVisited(mirrorDir string) (first bool) {
	if _, seen := e.visitedMirrorDirs[mirrorDir]; seen {
		return false
	}
	if e.visitedMirrorDirs == nil {
		e.visitedMirrorDirs = make(map[string]struct{})
	}
	e.visitedMirrorDirs[mirrorDir] = struct{}{}
	return true
}

// removeStaleGitMaintenanceFiles is the executor-side wrapper: it removes the
// files, reports them in the job log, and records the count and total size on
// the current span (git.mirror.update) so the frequency of interrupted gc runs
// can be measured. The attributes are set whenever the feature is enabled, so
// a zero count means "checked, nothing to remove" rather than "not checked".
func (e *Executor) removeStaleGitMaintenanceFiles(ctx context.Context, mirrorDir string) {
	removed, totalBytes, err := removeStaleGitMaintenanceFiles(mirrorDir)

	trace.SpanFromContext(ctx).SetAttributes(
		attribute.Int("git.mirror.stale_maintenance_files.count", len(removed)),
		attribute.Int64("git.mirror.stale_maintenance_files.bytes", totalBytes),
	)

	if err != nil {
		e.shell.Warningf("Could not remove stale Git maintenance files from mirror %q: %v", mirrorDir, err)
	}
	if len(removed) > 0 {
		e.shell.Commentf(
			"Removed %d file(s) (%s) left by an interrupted git gc or git maintenance from mirror %q: %s",
			len(removed), humanize.IBytes(uint64(totalBytes)), mirrorDir, strings.Join(removed, ", "),
		)
	} else if e.Debug {
		e.shell.Commentf("No stale Git maintenance files found in mirror %q", mirrorDir)
	}
}
