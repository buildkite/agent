package artifact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func findArtifact(artifacts []*api.Artifact, search string) *api.Artifact {
	for _, a := range artifacts {
		if filepath.Base(a.Path) == search {
			return a
		}
	}

	return nil
}

func TestCollect(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	root, _ := os.Getwd()

	volumeName := filepath.VolumeName(root)
	rootWithoutVolume := strings.TrimPrefix(root, volumeName)

	testCases := []struct {
		Name         string
		Path         []string
		AbsolutePath string
		FileSize     int
		Sha1Sum      string
		Sha256Sum    string
	}{
		{
			Name:         "Mr Freeze.jpg",
			Path:         []string{"fixtures", "Mr Freeze.jpg"},
			AbsolutePath: filepath.Join(root, "fixtures", "Mr Freeze.jpg"),
			FileSize:     362371,
			Sha1Sum:      "f5bc7bc9f5f9c3e543dde0eb44876c6f9acbfb6b",
			Sha256Sum:    "0c657a363d92093e68224e0716ed8b8b5d4bbc3dfe9b026e32b241fc9b369d47",
		},
		{
			Name:         "Commando.jpg",
			Path:         []string{"fixtures", "folder", "Commando.jpg"},
			AbsolutePath: filepath.Join(root, "fixtures", "folder", "Commando.jpg"),
			FileSize:     113000,
			Sha1Sum:      "811d7cb0317582e22ebfeb929d601cdabea4b3c0",
			Sha256Sum:    "fcfbe62fd7b6638165a61e8de901ac9df93fc1389906f2772bdefed5de115426",
		},
		{
			Name:         "The Terminator.jpg",
			Path:         []string{"fixtures", "this is a folder with a space", "The Terminator.jpg"},
			AbsolutePath: filepath.Join(root, "fixtures", "this is a folder with a space", "The Terminator.jpg"),
			FileSize:     47301,
			Sha1Sum:      "ed76566ede9cb6edc975fcadca429665aad8785a",
			Sha256Sum:    "5b4228a4bbef3d9f676e0a2e8cf6ea06759124ef0fbdb27a6c35df8759fcd39d",
		},
		{
			Name:         "Smile.gif",
			Path:         []string{rootWithoutVolume[1:], "fixtures", "gifs", "Smile.gif"},
			AbsolutePath: filepath.Join(root, "fixtures", "gifs", "Smile.gif"),
			FileSize:     2038453,
			Sha1Sum:      "bd4caf2e01e59777744ac1d52deafa01c2cb9bfd",
			Sha256Sum:    "fc5e8608c7772e4ae834fbc47eec3d902099eb3599f5191e40d9e3d9b3764b0e",
		},
	}

	uploader := NewUploader(logger.Discard, nil, UploaderConfig{
		Paths: fmt.Sprintf("%s;%s",
			filepath.Join("fixtures", "**/*.jpg"),
			filepath.Join(root, "fixtures", "**/*.gif"),
		),
		Delimiter: ";",
	})

	// For the normalised-upload-paths experiment, uploaded artifact paths are
	// normalised with Unix/URI style path separators, even on Windows.
	// Without the experiment on, we use the file path given by the file system
	//
	// To simulate that in this test, we collect artifacts from the file system
	// twice, once with the experiment explicitly disabled, and one with it
	// enabled. We then check the test cases against both sets of artifacts,
	// comparing to paths processed with filepath.Join (which uses native OS
	// path separators), and then with the experiment enabled and with the
	// path.Join function instead (which uses Unix/URI-style path separators,
	// regardless of platform)

	ctxExpEnabled, _ := experiments.Enable(ctx, experiments.NormalisedUploadPaths)
	ctxExpDisabled := experiments.Disable(ctx, experiments.NormalisedUploadPaths)

	artifactsWithoutExperimentEnabled, err := uploader.collect(ctxExpDisabled)
	if err != nil {
		t.Fatalf("[normalised-upload-paths disabled] uploader.Collect() error = %v", err)
	}
	if got, want := len(artifactsWithoutExperimentEnabled), 5; got != want {
		t.Errorf("len(artifactsWithoutExperimentEnabled) = %d, want %d", got, want)
	}

	artifactsWithExperimentEnabled, err := uploader.collect(ctxExpEnabled)
	if err != nil {
		t.Fatalf("[normalised-upload-paths enabled] uploader.Collect() error = %v", err)
	}
	if got, want := len(artifactsWithExperimentEnabled), 5; got != want {
		t.Errorf("len(artifactsWithExperimentEnabled) = %d, want %d", got, want)
	}

	// These test cases use filepath.Join, which uses per-OS path separators;
	// this is the behaviour without normalised-upload-paths.
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			a := findArtifact(artifactsWithoutExperimentEnabled, tc.Name)
			if a == nil {
				t.Fatalf("findArtifact(%q) == nil", tc.Name)
			}

			if got, want := a.Path, filepath.Join(tc.Path...); got != want {
				t.Errorf("a.Path = %q, want %q", got, want)
			}
			if got, want := a.AbsolutePath, tc.AbsolutePath; got != want {
				t.Errorf("a.AbsolutePath = %q, want %q", got, want)
			}
			if got, want := int(a.FileSize), tc.FileSize; got != want {
				t.Errorf("int(a.FileSize) = %d, want %d", got, want)
			}
			if got, want := a.Sha1Sum, tc.Sha1Sum; got != want {
				t.Errorf("a.Sha1Sum = %q, want %q", got, want)
			}
			if got, want := a.Sha256Sum, tc.Sha256Sum; got != want {
				t.Errorf("a.Sha256Sum = %q, want %q", got, want)
			}
		})
	}

	// These test cases uses filepath.ToSlash(), which always emits forward-slashes.
	// this is the behaviour with normalised-upload-paths.
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			a := findArtifact(artifactsWithExperimentEnabled, tc.Name)
			if a == nil {
				t.Fatalf("findArtifact(%q) == nil", tc.Name)
			}

			// Note that the rootWithoutVolume component of some tc.Path values
			// may already have backslashes in them on Windows:
			// []string{"path\to\codebase", "fixtures", "hello"}
			// So forward-slash joining them with path.Join(tc.Path...} isn't enough.
			forwardSlashed := filepath.ToSlash(filepath.Join(tc.Path...))

			if got, want := a.Path, forwardSlashed; got != want {
				t.Errorf("a.Path = %q, want %q", got, want)
			}
			if got, want := a.AbsolutePath, tc.AbsolutePath; got != want {
				t.Errorf("a.AbsolutePath = %q, want %q", got, want)
			}
			if got, want := int(a.FileSize), tc.FileSize; got != want {
				t.Errorf("int(a.FileSize) = %d, want %d", got, want)
			}
			if got, want := a.Sha1Sum, tc.Sha1Sum; got != want {
				t.Errorf("a.Sha1Sum = %q, want %q", got, want)
			}
			if got, want := a.Sha256Sum, tc.Sha256Sum; got != want {
				t.Errorf("a.Sha256Sum = %q, want %q", got, want)
			}
		})
	}
}

func TestCollectThatDoesntMatchAnyFiles(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	uploader := NewUploader(logger.Discard, nil, UploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("log", "*"),
			filepath.Join("tmp", "capybara", "**", "*"),
			filepath.Join("mkmf.log"),
			filepath.Join("log", "mkmf.log"),
		}, ";"),
		Delimiter: ";",
	})

	artifacts, err := uploader.collect(ctx)
	if err != nil {
		t.Fatalf("uploader.Collect() error = %v", err)
	}

	if got, want := len(artifacts), 0; got != want {
		t.Errorf("len(artifacts) = %d, want %d", got, want)
	}
}

func TestCollectWithSomeGlobsThatDontMatchAnything(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	uploader := NewUploader(logger.Discard, nil, UploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("dontmatchanything", "*"),
			filepath.Join("dontmatchanything.zip"),
			filepath.Join("fixtures", "**", "*.jpg"),
		}, ";"),
		Delimiter: ";",
	})

	artifacts, err := uploader.collect(ctx)
	if err != nil {
		t.Fatalf("uploader.Collect() error = %v", err)
	}

	if len(artifacts) != 4 {
		t.Errorf("len(artifacts) = %d, want 4", len(artifacts))
	}
}

func TestCollectWithSomeGlobsThatDontMatchAnythingFollowingSymlinks(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	uploader := NewUploader(logger.Discard, nil, UploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("dontmatchanything", "*"),
			filepath.Join("dontmatchanything.zip"),
			filepath.Join("fixtures", "links", "folder-link", "dontmatchanything", "**", "*.jpg"),
			filepath.Join("fixtures", "**", "*.jpg"),
		}, ";"),
		Delimiter:                 ";",
		GlobResolveFollowSymlinks: true,
	})

	artifacts, err := uploader.collect(ctx)
	if err != nil {
		t.Fatalf("uploader.Collect() error = %v", err)
	}

	if len(artifacts) != 5 {
		t.Errorf("len(artifacts) = %d, want 5", len(artifacts))
	}
}

func TestCollectWithDuplicateMatches(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	uploader := NewUploader(logger.Discard, nil, UploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("fixtures", "**", "*.jpg"),
			filepath.Join("fixtures", "folder", "Commando.jpg"), // dupe
		}, ";"),
		Delimiter: ";",
	})

	artifacts, err := uploader.collect(ctx)
	if err != nil {
		t.Fatalf("uploader.Collect() error = %v", err)
	}

	paths := []string{}
	for _, a := range artifacts {
		paths = append(paths, a.Path)
	}
	if diff := cmp.Diff(slices.Sorted(slices.Values(paths)), slices.Sorted(slices.Values([]string{
		filepath.Join("fixtures", "Mr Freeze.jpg"),
		filepath.Join("fixtures", "folder", "Commando.jpg"),
		filepath.Join("fixtures", "this is a folder with a space", "The Terminator.jpg"),
		filepath.Join("fixtures", "links", "terminator", "terminator2.jpg"),
	}))); diff != "" {
		t.Errorf("paths sorted diff (-got +want):\n%s", diff)
	}
}

func TestCollectWithDuplicateMatchesFollowingSymlinks(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	uploader := NewUploader(logger.Discard, nil, UploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("fixtures", "**", "*.jpg"),
			filepath.Join("fixtures", "folder", "Commando.jpg"), // dupe
		}, ";"),
		Delimiter:                 ";",
		GlobResolveFollowSymlinks: true,
	})

	artifacts, err := uploader.collect(ctx)
	if err != nil {
		t.Fatalf("uploader.Collect() error = %v", err)
	}

	paths := []string{}
	for _, a := range artifacts {
		paths = append(paths, a.Path)
	}
	if diff := cmp.Diff(slices.Sorted(slices.Values(paths)), slices.Sorted(slices.Values([]string{
		filepath.Join("fixtures", "Mr Freeze.jpg"),
		filepath.Join("fixtures", "folder", "Commando.jpg"),
		filepath.Join("fixtures", "this is a folder with a space", "The Terminator.jpg"),
		filepath.Join("fixtures", "links", "terminator", "terminator2.jpg"),
		filepath.Join("fixtures", "links", "folder-link", "terminator2.jpg"),
	}))); diff != "" {
		t.Errorf("paths sorted diff (-got +want):\n%s", diff)
	}
}

func TestCollectMatchesUploadSymlinks(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	uploader := NewUploader(logger.Discard, nil, UploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("fixtures", "**", "*.jpg"),
		}, ";"),
		Delimiter:          ";",
		UploadSkipSymlinks: true,
	})

	artifacts, err := uploader.collect(ctx)
	if err != nil {
		t.Fatalf("uploader.Collect() error = %v", err)
	}

	paths := []string{}
	for _, a := range artifacts {
		paths = append(paths, a.Path)
	}
	if diff := cmp.Diff(slices.Sorted(slices.Values(paths)), slices.Sorted(slices.Values([]string{
		filepath.Join("fixtures", "Mr Freeze.jpg"),
		filepath.Join("fixtures", "folder", "Commando.jpg"),
		filepath.Join("fixtures", "this is a folder with a space", "The Terminator.jpg"),
	}))); diff != "" {
		t.Errorf("paths sorted diff (-got +want):\n%s", diff)
	}
}

func TestCollect_Literal(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	uploader := NewUploader(logger.Discard, nil, UploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("fixtures", "links", "folder-link", "terminator2.jpg"),
			filepath.Join("fixtures", "gifs", "Smile.gif"),
		}, ";"),
		Delimiter: ";",
		Literal:   true,
	})

	artifacts, err := uploader.collect(ctx)
	if err != nil {
		t.Fatalf("uploader.Collect() error = %v", err)
	}

	got := []string{}
	for _, a := range artifacts {
		got = append(got, a.Path)
	}
	want := []string{
		filepath.Join("fixtures", "links", "folder-link", "terminator2.jpg"),
		filepath.Join("fixtures", "gifs", "Smile.gif"),
	}
	slices.Sort(got)
	slices.Sort(want)
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("uploader.collect artifact paths diff (-got +want)\n%s", diff)
	}
}

func TestCollect_LiteralPathNotFound(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	uploader := NewUploader(logger.Discard, nil, UploaderConfig{
		// When parsed as a glob, it finds multiple files.
		// When used literally, it finds nothing.
		Paths:   filepath.Join("fixtures", "**", "*.jpg"),
		Literal: true,
	})

	var pathErr *os.PathError
	if _, err := uploader.collect(ctx); !errors.As(err, &pathErr) {
		t.Fatalf("uploader.collect() error = %v, want %T", err, pathErr)
	}
	if pathErr.Op != "open" {
		t.Errorf("uploader.collect() error Op = %q, want open", pathErr.Op)
	}
}

func TestUploadUsesConfiguredConcurrency(t *testing.T) {
	t.Parallel()

	const wantConcurrency int32 = 3

	state := &uploadConcurrencyState{
		want:    wantConcurrency,
		ready:   make(chan struct{}),
		release: make(chan struct{}),
	}
	uploader := NewUploader(logger.Discard, &uploadConcurrencyAPIClient{}, UploaderConfig{
		JobID:             "job-id",
		UploadConcurrency: int(wantConcurrency),
	})
	workCreator := &uploadConcurrencyWorkCreator{state: state}

	artifacts := make([]*api.Artifact, 10)
	for i := range artifacts {
		artifacts[i] = &api.Artifact{
			ID:   fmt.Sprintf("artifact-%d", i),
			Path: fmt.Sprintf("artifact-%d.txt", i),
		}
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	errCh := make(chan struct {
		stats artifactUploadBatchStats
		err   error
	}, 1)
	go func() {
		stats, err := uploader.upload(ctx, artifacts, workCreator)
		errCh <- struct {
			stats artifactUploadBatchStats
			err   error
		}{stats, err}
	}()

	select {
	case <-state.ready:
	case <-time.After(2 * time.Second):
		cancel()
		result := <-errCh
		t.Fatalf("upload did not start %d concurrent workers before timeout: %v", wantConcurrency, result.err)
	}

	close(state.release)

	result := <-errCh
	if result.err != nil {
		t.Fatalf("uploader.upload() error = %v", result.err)
	}

	if got := state.max.Load(); got != wantConcurrency {
		t.Fatalf("max concurrent uploads = %d, want %d", got, wantConcurrency)
	}
	if got, want := result.stats.workUnits, len(artifacts); got != want {
		t.Fatalf("upload stats workUnits = %d, want %d", got, want)
	}
	if got := result.stats.workerCount; got != int(wantConcurrency) {
		t.Fatalf("upload stats workerCount = %d, want %d", got, wantConcurrency)
	}
	if got := result.stats.stateUpdateCount; got != 1 {
		t.Fatalf("upload stats stateUpdateCount = %d, want 1", got)
	}
}

func TestDefaultUploadConcurrencyMatchesExistingWorkerCount(t *testing.T) {
	t.Parallel()

	if got, want := DefaultUploadConcurrency(), runtime.GOMAXPROCS(0); got != want {
		t.Fatalf("DefaultUploadConcurrency() = %d, want %d", got, want)
	}
}

func TestArtifactUploadTimingsSummary(t *testing.T) {
	t.Parallel()

	timings := &artifactUploadTimings{
		collectDuration: 1456 * time.Millisecond,
		createDuration:  82 * time.Millisecond,
		uploadDuration:  334 * time.Millisecond,
		stateDuration:   11 * time.Millisecond,

		artifactCount:    1296,
		artifactBytes:    1296 * 4096,
		batches:          44,
		workUnits:        1296,
		maxWorkerCount:   30,
		stateUpdateCount: 44,
	}

	l := logger.NewBuffer()
	timings.logSummary(l)

	got := strings.Join(l.Messages, "\n")
	for _, want := range []string{
		"Artifact upload timings:",
		"collect=1.456s",
		"create=82ms",
		"upload=334ms",
		"state_update=11ms",
		"artifacts=1296",
		"bytes=5.1 MiB",
		"batches=44",
		"work_units=1296",
		"max_workers=30",
		"state_updates=44",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("timings summary = %q, want substring %q", got, want)
		}
	}
}

func TestArtifactUploadTimingsSpanAttributes(t *testing.T) {
	t.Parallel()

	timings := &artifactUploadTimings{
		collectDuration: 1456 * time.Millisecond,
		createDuration:  82 * time.Millisecond,
		uploadDuration:  334 * time.Millisecond,
		stateDuration:   11 * time.Millisecond,

		artifactCount:    1296,
		artifactBytes:    1296 * 4096,
		batches:          44,
		workUnits:        1296,
		maxWorkerCount:   30,
		stateUpdateCount: 44,
	}

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) }) //nolint:usetesting // t.Context() is cancelled before Cleanup funcs

	ctx, span := provider.Tracer("test").Start(t.Context(), "artifact-upload")
	timings.setSpanAttributes(ctx)
	span.End()

	spans := recorder.Ended()
	if got, want := len(spans), 1; got != want {
		t.Fatalf("ended spans = %d, want %d", got, want)
	}

	got := make(map[string]any)
	for _, attr := range spans[0].Attributes() {
		got[string(attr.Key)] = attr.Value.AsInterface()
	}

	want := map[string]any{
		"artifact.count":              int64(1296),
		"artifact.bytes":              int64(1296 * 4096),
		"artifact.batch_count":        int64(44),
		"artifact.work_unit_count":    int64(1296),
		"artifact.max_workers":        int64(30),
		"artifact.state_update_count": int64(44),
		"artifact.collect_ms":         int64(1456),
		"artifact.create_ms":          int64(82),
		"artifact.upload_ms":          int64(334),
		"artifact.state_update_ms":    int64(11),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("span attributes diff (-want +got):\n%s", diff)
	}
}

type uploadConcurrencyState struct {
	want int32

	active atomic.Int32
	max    atomic.Int32

	readyOnce sync.Once
	ready     chan struct{}
	release   chan struct{}
}

type uploadConcurrencyWorkCreator struct {
	state *uploadConcurrencyState
}

func (u *uploadConcurrencyWorkCreator) URL(*api.Artifact) string {
	return ""
}

func (u *uploadConcurrencyWorkCreator) CreateWork(artifact *api.Artifact) ([]workUnit, error) {
	return []workUnit{&uploadConcurrencyWork{
		artifact: artifact,
		state:    u.state,
	}}, nil
}

type uploadConcurrencyWork struct {
	artifact *api.Artifact
	state    *uploadConcurrencyState
}

func (u *uploadConcurrencyWork) Artifact() *api.Artifact {
	return u.artifact
}

func (u *uploadConcurrencyWork) Description() string {
	return u.artifact.Path
}

func (u *uploadConcurrencyWork) DoWork(ctx context.Context) (*api.ArtifactPartETag, error) {
	active := u.state.active.Add(1)
	for {
		maxActive := u.state.max.Load()
		if active <= maxActive || u.state.max.CompareAndSwap(maxActive, active) {
			break
		}
	}
	if active == u.state.want {
		u.state.readyOnce.Do(func() { close(u.state.ready) })
	}
	defer u.state.active.Add(-1)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-u.state.release:
		return nil, nil
	}
}

type uploadConcurrencyAPIClient struct{}

func (*uploadConcurrencyAPIClient) CreateArtifacts(context.Context, string, *api.ArtifactBatch) (*api.ArtifactBatchCreateResponse, *api.Response, error) {
	return nil, nil, errors.New("unexpected CreateArtifacts call")
}

func (*uploadConcurrencyAPIClient) SearchArtifacts(context.Context, string, *api.ArtifactSearchOptions) ([]*api.Artifact, *api.Response, error) {
	return nil, nil, errors.New("unexpected SearchArtifacts call")
}

func (*uploadConcurrencyAPIClient) UpdateArtifacts(context.Context, string, []api.ArtifactState) (*api.Response, error) {
	return nil, nil
}
