package job

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/tracetools"
	"go.opentelemetry.io/otel"
)

// writeMirrorFile writes a file of the given size (relative to mirrorDir),
// creating parent directories as needed.
func writeMirrorFile(t *testing.T, mirrorDir, rel string, size int, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(mirrorDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), mode); err != nil {
		t.Fatal(err)
	}
}

func mirrorFileExists(t *testing.T, mirrorDir, rel string) bool {
	t.Helper()
	_, err := os.Lstat(filepath.Join(mirrorDir, filepath.FromSlash(rel)))
	if err == nil {
		return true
	}
	if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", rel, err)
	}
	return false
}

func TestRemoveStaleGitMaintenanceFiles(t *testing.T) {
	t.Parallel()

	mirrorDir := t.TempDir()

	// What a gc killed mid-repack leaves behind. Sizes are distinct so a wrong
	// total can't come out right by coincidence. The finished pack is read-only,
	// as git writes it.
	stale := map[string]int{
		"objects/pack/tmp_pack_AbC123":                  1000,
		"objects/pack/tmp_idx_dEf456":                   100,
		"objects/pack/tmp_rev_GhI789":                   10,
		"objects/pack/.tmp-4242-pack-0123abcd.pack":     2000,
		"objects/pack/.tmp-4242-pack-0123abcd.idx":      200,
		"objects/pack/.tmp-4242-pack-0123abcd.promisor": 0,
		"gc.pid":                   15,
		"gc.log.lock":              0,
		"objects/maintenance.lock": 0,
	}
	for rel, size := range stale {
		writeMirrorFile(t, mirrorDir, rel, size, 0o644)
	}
	if err := os.Chmod(filepath.Join(mirrorDir, "objects", "pack", "tmp_pack_AbC123"), 0o444); err != nil {
		t.Fatal(err)
	}

	// Everything else in a mirror must survive, including things with similar
	// names: real packs, keep files, the MIDX, gc.log (a completed gc's error
	// report, which git expires itself), and tmp_ files outside objects/pack.
	keep := []string{
		"HEAD",
		"config",
		"packed-refs",
		"gc.log",
		"objects/info/alternates",
		"objects/pack/pack-0123abcd.pack",
		"objects/pack/pack-0123abcd.idx",
		"objects/pack/pack-0123abcd.keep",
		"objects/pack/multi-pack-index",
		"objects/pack/multi-pack-index.lock",
		"objects/pack/tmp",
		"objects/tmp_obj_zzz",
		"objects/pack/.tmp-not-a-pack",
	}
	for _, rel := range keep {
		writeMirrorFile(t, mirrorDir, rel, 1, 0o644)
	}

	removed, totalBytes, err := removeStaleGitMaintenanceFiles(mirrorDir)
	if err != nil {
		t.Fatalf("removeStaleGitMaintenanceFiles() error = %v", err)
	}

	var wantRemoved []string
	var wantBytes int64
	for rel, size := range stale {
		wantRemoved = append(wantRemoved, filepath.FromSlash(rel))
		wantBytes += int64(size)
	}
	slices.Sort(wantRemoved)
	slices.Sort(removed)
	if !slices.Equal(removed, wantRemoved) {
		t.Errorf("removed = %v, want %v", removed, wantRemoved)
	}
	if totalBytes != wantBytes {
		t.Errorf("totalBytes = %d, want %d", totalBytes, wantBytes)
	}
	for rel := range stale {
		if mirrorFileExists(t, mirrorDir, rel) {
			t.Errorf("%s still exists", rel)
		}
	}
	for _, rel := range keep {
		if !mirrorFileExists(t, mirrorDir, rel) {
			t.Errorf("%s was removed", rel)
		}
	}
}

func TestRemoveStaleGitMaintenanceFiles_NothingToDo(t *testing.T) {
	t.Parallel()

	// A mirror that has never been repacked has no objects/pack at all.
	mirrorDir := t.TempDir()
	writeMirrorFile(t, mirrorDir, "HEAD", 1, 0o644)

	removed, totalBytes, err := removeStaleGitMaintenanceFiles(mirrorDir)
	if err != nil {
		t.Fatalf("removeStaleGitMaintenanceFiles() error = %v", err)
	}
	if len(removed) != 0 || totalBytes != 0 {
		t.Errorf("removed = %v, totalBytes = %d, want nothing", removed, totalBytes)
	}
}

func TestRemoveStaleGitMaintenanceFiles_ContinuesAfterFailure(t *testing.T) {
	t.Parallel()

	mirrorDir := t.TempDir()
	// A non-empty directory where gc.pid should be can't be removed with
	// os.Remove; the other leftovers must still go.
	writeMirrorFile(t, mirrorDir, "gc.pid/child", 1, 0o644)
	writeMirrorFile(t, mirrorDir, "gc.log.lock", 0, 0o644)
	writeMirrorFile(t, mirrorDir, "objects/pack/tmp_pack_AbC123", 7, 0o644)

	removed, totalBytes, err := removeStaleGitMaintenanceFiles(mirrorDir)
	if err == nil {
		t.Fatal("removeStaleGitMaintenanceFiles() error = nil, want error for gc.pid")
	}
	if !strings.Contains(err.Error(), "gc.pid") {
		t.Errorf("error = %v, want it to name gc.pid", err)
	}
	slices.Sort(removed)
	want := []string{"gc.log.lock", filepath.Join("objects", "pack", "tmp_pack_AbC123")}
	if !slices.Equal(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}
	if totalBytes != 7 {
		t.Errorf("totalBytes = %d, want 7", totalBytes)
	}
	if !mirrorFileExists(t, mirrorDir, "gc.pid/child") {
		t.Error("gc.pid/child was removed")
	}
}

func TestIsStaleGitPackFile(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"tmp_pack_AbC123":                 true,
		"tmp_idx_AbC123":                  true,
		"tmp_rev_AbC123":                  true,
		"tmp_mtimes_AbC123":               true,
		"tmp_bitmap_AbC123":               true,
		".tmp-4242-pack-0123abcd.pack":    true,
		".tmp-4242-pack-0123abcd.idx":     true,
		".tmp-4242-pack-0123abcd.bitmap":  true,
		"pack-0123abcd.pack":              false,
		"pack-0123abcd.keep":              false,
		"multi-pack-index":                false,
		"multi-pack-index.lock":           false,
		".tmp-not-a-pack":                 false,
		".tmp-x-pack-0123abcd.pack":       false,
		"tmp-4242-pack-0123abcd.pack":     false,
		"tmp":                             false,
		"my_tmp_pack":                     false,
		"pack-0123abcd.pack.tmp_pack_xyz": false,
	} {
		if got := isStaleGitPackFile(name); got != want {
			t.Errorf("isStaleGitPackFile(%q) = %t, want %t", name, got, want)
		}
	}
}

// seedStaleMaintenanceFiles plants the leftovers of a killed gc into an
// existing mirror and returns their total size.
func seedStaleMaintenanceFiles(t *testing.T, mirrorDir string) int64 {
	t.Helper()
	files := map[string]int{
		"objects/pack/tmp_pack_KiLLed": 4096,
		"gc.pid":                       15,
		"objects/maintenance.lock":     0,
	}
	var total int64
	for rel, size := range files {
		writeMirrorFile(t, mirrorDir, rel, size, 0o644)
		total += int64(size)
	}
	return total
}

// seededStaleMaintenanceFiles are the paths seedStaleMaintenanceFiles writes.
var seededStaleMaintenanceFiles = []string{"objects/pack/tmp_pack_KiLLed", "gc.pid", "objects/maintenance.lock"}

// newMirrorMaintenanceJob returns an executor for a fresh job that reuses the
// mirrors path of a previous one, as a later Hosted job on the same cache
// volume would, with the job log captured in the returned buffer.
func newMirrorMaintenanceJob(t *testing.T, previous *Executor) (*Executor, *bytes.Buffer) {
	t.Helper()
	e := newOnHostMirrorExecutor(t, previous.Repository, previous.Commit)
	e.GitMirrorsPath = previous.GitMirrorsPath
	e.GitMirrorsRemoveStaleMaintenanceFiles = true
	e.TracingBackend = tracetools.BackendOpenTelemetry
	logs := &bytes.Buffer{}
	var err error
	e.shell, err = shell.New(
		shell.WithEnv(e.shell.Env),
		shell.WithLogger(shell.NewWriterLogger(logs, false, nil)),
		shell.WithStdout(&process.Buffer{}),
		shell.WithSignalGracePeriod(10*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	return e, logs
}

// updateMirrorInSpan runs updateGitMirror inside a span standing in for
// git.mirror.update, which the checkout phase would normally provide.
func updateMirrorInSpan(t *testing.T, e *Executor) {
	t.Helper()
	ctx, span := otel.Tracer("test").Start(t.Context(), "git.mirror.update")
	_, err := e.updateGitMirror(ctx, e.Repository, nil)
	span.End()
	if err != nil {
		t.Fatalf("updateGitMirror() error = %v", err)
	}
}

func TestUpdateGitMirrorRemovesStaleMaintenanceFilesWhenEnabled(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}

	// Job 1 clones the mirror and is then cut off mid-gc.
	first := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), commit)
	first.GitMirrorsRemoveStaleMaintenanceFiles = true
	if _, err := first.updateGitMirror(t.Context(), first.Repository, nil); err != nil {
		t.Fatalf("updateGitMirror() (clone) error = %v", err)
	}
	mirrorDir := expectedOnHostMirrorDir(first)
	wantBytes := seedStaleMaintenanceFiles(t, mirrorDir)

	// Job 2 finds the leftovers on its first use of the mirror, which is the
	// one place they are known not to belong to a gc this job started.
	e, logs := newMirrorMaintenanceJob(t, first)
	recorder := installGlobalSpanRecorder(t)
	updateMirrorInSpan(t, e)

	for _, rel := range seededStaleMaintenanceFiles {
		if mirrorFileExists(t, mirrorDir, rel) {
			t.Errorf("%s still exists in mirror", rel)
		}
	}
	// The mirror must still be a working repository afterwards.
	if got := gitOutputForMirrorTest(t, mirrorDir, "rev-parse", commit); got != commit {
		t.Errorf("mirror rev-parse = %q, want %q", got, commit)
	}

	wantLog := "Removed 3 file(s) (4.0 KiB) left by an interrupted git gc or git maintenance from mirror"
	if !strings.Contains(logs.String(), wantLog) {
		t.Errorf("job log does not contain %q:\n%s", wantLog, logs.String())
	}
	for _, rel := range []string{"tmp_pack_KiLLed", "gc.pid", "maintenance.lock"} {
		if !strings.Contains(logs.String(), rel) {
			t.Errorf("job log does not name %s:\n%s", rel, logs.String())
		}
	}

	var found bool
	for _, s := range recorder.Ended() {
		if s.Name() != "git.mirror.update" {
			continue
		}
		found = true
		assertSpanAttr(t, s, "git.mirror.stale_maintenance_files.count", "3")
		assertSpanAttr(t, s, "git.mirror.stale_maintenance_files.bytes", strconv.FormatInt(wantBytes, 10))
	}
	if !found {
		t.Error("no git.mirror.update span recorded")
	}
}

func TestUpdateGitMirrorKeepsMaintenanceFilesOnRepeatVisitInSameJob(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}

	first := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), commit)
	if _, err := first.updateGitMirror(t.Context(), first.Repository, nil); err != nil {
		t.Fatalf("updateGitMirror() (clone) error = %v", err)
	}
	mirrorDir := expectedOnHostMirrorDir(first)

	e, logs := newMirrorMaintenanceJob(t, first)
	updateMirrorInSpan(t, e)

	// The fetch above could have started a detached gc that is still writing
	// these. A checkout retry or a repeated submodule URL updates the same
	// mirror again within the job and must leave them alone.
	seedStaleMaintenanceFiles(t, mirrorDir)
	recorder := installGlobalSpanRecorder(t)
	logs.Reset()
	updateMirrorInSpan(t, e)

	for _, rel := range seededStaleMaintenanceFiles {
		if !mirrorFileExists(t, mirrorDir, rel) {
			t.Errorf("%s was removed on a repeat visit to the mirror", rel)
		}
	}
	if strings.Contains(logs.String(), "left by an interrupted git gc") {
		t.Errorf("job log reports a removal on a repeat visit:\n%s", logs.String())
	}
	for _, s := range recorder.Ended() {
		if s.Name() != "git.mirror.update" {
			continue
		}
		if _, ok := spanAttr(s, "git.mirror.stale_maintenance_files.count"); ok {
			t.Error("git.mirror.stale_maintenance_files.count set on a repeat visit")
		}
	}

	// A different job sees the mirror for the first time and does clean up.
	next, _ := newMirrorMaintenanceJob(t, first)
	updateMirrorInSpan(t, next)
	for _, rel := range seededStaleMaintenanceFiles {
		if mirrorFileExists(t, mirrorDir, rel) {
			t.Errorf("%s still exists after a new job's first visit", rel)
		}
	}
}

func TestUpdateGitMirrorKeepsStaleMaintenanceFilesByDefault(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}

	first := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), commit)
	if _, err := first.updateGitMirror(t.Context(), first.Repository, nil); err != nil {
		t.Fatalf("updateGitMirror() (clone) error = %v", err)
	}
	mirrorDir := expectedOnHostMirrorDir(first)
	seedStaleMaintenanceFiles(t, mirrorDir)

	// A fresh job with the flag at its default, so this is its first visit
	// and the only reason to keep the files is the flag.
	e, _ := newMirrorMaintenanceJob(t, first)
	e.GitMirrorsRemoveStaleMaintenanceFiles = false
	recorder := installGlobalSpanRecorder(t)
	updateMirrorInSpan(t, e)

	for _, rel := range seededStaleMaintenanceFiles {
		if !mirrorFileExists(t, mirrorDir, rel) {
			t.Errorf("%s was removed from mirror with the flag off", rel)
		}
	}
	// With the feature off, the span must not claim a zero count: absent
	// attributes mean "not checked".
	for _, s := range recorder.Ended() {
		if s.Name() != "git.mirror.update" {
			continue
		}
		if _, ok := spanAttr(s, "git.mirror.stale_maintenance_files.count"); ok {
			t.Error("git.mirror.stale_maintenance_files.count set on span with the flag off")
		}
	}
}

func TestRemoveFile_ReadOnly(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		// On POSIX a read-only file is removable anyway; only Windows refuses.
		t.Skip("read-only files are removable on " + runtime.GOOS)
	}
	path := filepath.Join(t.TempDir(), "tmp_pack_ro")
	if err := os.WriteFile(path, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := removeFile(path, info); err != nil {
		t.Fatalf("removeFile() error = %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists: %v", err)
	}
}
