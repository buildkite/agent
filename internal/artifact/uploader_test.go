package artifact

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
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
		FileSize     int64
		Sha1Sum      string
		Sha256Sum    string
		ContentType  string
	}{
		{
			Name:         "Mr Freeze.jpg",
			Path:         []string{"fixtures", "Mr Freeze.jpg"},
			AbsolutePath: filepath.Join(root, "fixtures", "Mr Freeze.jpg"),
			FileSize:     362371,
			Sha1Sum:      "f5bc7bc9f5f9c3e543dde0eb44876c6f9acbfb6b",
			Sha256Sum:    "0c657a363d92093e68224e0716ed8b8b5d4bbc3dfe9b026e32b241fc9b369d47",
			ContentType:  "image/jpeg",
		},
		{
			Name:         "Commando.jpg",
			Path:         []string{"fixtures", "folder", "Commando.jpg"},
			AbsolutePath: filepath.Join(root, "fixtures", "folder", "Commando.jpg"),
			FileSize:     113000,
			Sha1Sum:      "811d7cb0317582e22ebfeb929d601cdabea4b3c0",
			Sha256Sum:    "fcfbe62fd7b6638165a61e8de901ac9df93fc1389906f2772bdefed5de115426",
			ContentType:  "image/jpeg",
		},
		{
			Name:         "The Terminator.jpg",
			Path:         []string{"fixtures", "this is a folder with a space", "The Terminator.jpg"},
			AbsolutePath: filepath.Join(root, "fixtures", "this is a folder with a space", "The Terminator.jpg"),
			FileSize:     47301,
			Sha1Sum:      "ed76566ede9cb6edc975fcadca429665aad8785a",
			Sha256Sum:    "5b4228a4bbef3d9f676e0a2e8cf6ea06759124ef0fbdb27a6c35df8759fcd39d",
			ContentType:  "image/jpeg",
		},
		{
			Name:         "Smile.gif",
			Path:         []string{rootWithoutVolume[1:], "fixtures", "gifs", "Smile.gif"},
			AbsolutePath: filepath.Join(root, "fixtures", "gifs", "Smile.gif"),
			FileSize:     2038453,
			Sha1Sum:      "bd4caf2e01e59777744ac1d52deafa01c2cb9bfd",
			Sha256Sum:    "fc5e8608c7772e4ae834fbc47eec3d902099eb3599f5191e40d9e3d9b3764b0e",
			ContentType:  "image/gif",
		},
	}

	uploader := NewUploader(logger.Discard, nil, UploaderConfig{
		Paths: fmt.Sprintf("%s;%s",
			filepath.Join("fixtures", "**/*.jpg"),
			filepath.Join(root, "fixtures", "**/*.gif"),
		),
		Delimiter: ";",
	})

	artifacts, err := uploader.collect(ctx)
	if err != nil {
		t.Fatalf("uploader.Collect() error = %v", err)
	}
	if got, want := len(artifacts), 5; got != want {
		t.Errorf("len(artifacts) = %d, want %d", got, want)
	}

	// These test cases uses filepath.ToSlash(), which always emits forward-slashes.
	// this is the behaviour with normalised-upload-paths.
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			got := findArtifact(artifacts, tc.Name)
			if got == nil {
				t.Fatalf("findArtifact(%q) == nil", tc.Name)
			}

			// Note that the rootWithoutVolume component of some tc.Path values
			// may already have backslashes in them on Windows:
			// []string{"path\to\codebase", "fixtures", "hello"}
			// So forward-slash joining them with path.Join(tc.Path...} isn't enough.
			forwardSlashed := filepath.ToSlash(filepath.Join(tc.Path...))

			want := &api.Artifact{
				Path:         forwardSlashed,
				AbsolutePath: tc.AbsolutePath,
				FileSize:     tc.FileSize,
				Sha1Sum:      tc.Sha1Sum,
				Sha256Sum:    tc.Sha256Sum,
				ContentType:  tc.ContentType,
			}

			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("findArtifact diff (-got +want):\n%s", diff)
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

	got := []string{}
	for _, a := range artifacts {
		got = append(got, a.Path)
	}
	slices.Sort(got)
	want := []string{
		"fixtures/Mr Freeze.jpg",
		"fixtures/folder/Commando.jpg",
		"fixtures/links/folder-link/terminator2.jpg",
		"fixtures/links/terminator/terminator2.jpg",
		"fixtures/this is a folder with a space/The Terminator.jpg",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("uploader.collect(ctx) collected paths diff (-got +want):\n%s", diff)
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

	got := []string{}
	for _, a := range artifacts {
		got = append(got, a.Path)
	}
	slices.Sort(got)
	want := []string{
		"fixtures/Mr Freeze.jpg",
		"fixtures/folder/Commando.jpg",
		"fixtures/links/terminator/terminator2.jpg",
		"fixtures/this is a folder with a space/The Terminator.jpg",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("uploader.collect(ctx) collected paths diff (-got +want):\n%s", diff)
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

	got := []string{}
	for _, a := range artifacts {
		got = append(got, a.Path)
	}
	slices.Sort(got)
	want := []string{
		"fixtures/Mr Freeze.jpg",
		"fixtures/folder/Commando.jpg",
		"fixtures/links/folder-link/terminator2.jpg",
		"fixtures/links/terminator/terminator2.jpg",
		"fixtures/this is a folder with a space/The Terminator.jpg",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("uploader.collect(ctx) collected paths diff (-got +want):\n%s", diff)
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

	got := []string{}
	for _, a := range artifacts {
		got = append(got, a.Path)
	}
	slices.Sort(got)
	want := []string{
		"fixtures/Mr Freeze.jpg",
		"fixtures/folder/Commando.jpg",
		"fixtures/this is a folder with a space/The Terminator.jpg",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("uploader.collect(ctx) collected paths diff (-got +want):\n%s", diff)
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
		"fixtures/links/folder-link/terminator2.jpg",
		"fixtures/gifs/Smile.gif",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("uploader.collect artifact paths diff (-got +want) (-got +want)\n%s", diff)
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
