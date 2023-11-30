package tempfile_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/tempfile"
	"gotest.tools/v3/assert"
)

func TestNew(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		opts  []tempfile.Opts
		check func(t *testing.T, f *os.File)
	}{
		{
			name:  "default",
			opts:  []tempfile.Opts{},
			check: func(t *testing.T, f *os.File) {},
		},
		{
			name: "with filename",
			opts: []tempfile.Opts{
				tempfile.WithName("foo.txt"),
			},
			check: func(t *testing.T, f *os.File) {},
		},
		{
			name: "with dir",
			opts: []tempfile.Opts{
				tempfile.WithDir("foo"),
			},
			check: func(t *testing.T, f *os.File) {
				assert.Check(t, strings.HasPrefix(f.Name(), filepath.Join(os.TempDir(), "foo")))
			},
		},
		{
			name: "with perms",
			opts: []tempfile.Opts{
				tempfile.WithPerms(0o600),
			},
			check: func(t *testing.T, f *os.File) {
				if runtime.GOOS == "windows" {
					t.Skip("Windows doesn't support or need checking if chmod worked")
				}

				fi, err := os.Stat(f.Name())
				assert.NilError(t, err, "os.Stat(%q) = %s", f.Name(), err)
				assert.Check(t, fi.Mode().Perm() == os.FileMode(0o600))
			},
		},
		{
			name: "with filename and keep extension",
			opts: []tempfile.Opts{
				tempfile.WithName("foo.txt"),
				tempfile.KeepingExtension(),
			},
			check: func(t *testing.T, f *os.File) {
				assert.Check(t, filepath.Ext(f.Name()) == ".txt")
			},
		},
		{
			name: "without filename and keep extension",
			opts: []tempfile.Opts{
				tempfile.KeepingExtension(),
			},
			check: func(t *testing.T, f *os.File) {},
		},
	} {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			f, err := tempfile.New(test.opts...)
			assert.NilError(t, err, `New(%v) = %v`, test.opts, err)

			t.Cleanup(func() {
				assert.Check(t, f.Close() == nil, "failed to close file: %s", f.Name())
				assert.Check(t, os.Remove(f.Name()) == nil, "failed to remove file: %s", f.Name())
			})

			assert.Check(t, strings.HasPrefix(f.Name(), os.TempDir()))
			test.check(t, f)
		})
	}
}

func TestNewClosed(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		opts  []tempfile.Opts
		check func(t *testing.T, filename string)
	}{
		{
			name:  "default",
			opts:  []tempfile.Opts{},
			check: func(t *testing.T, filename string) {},
		},
		{
			name: "with filename",
			opts: []tempfile.Opts{
				tempfile.WithName("foo.txt"),
			},
			check: func(t *testing.T, filename string) {},
		},
		{
			name: "with dir",
			opts: []tempfile.Opts{
				tempfile.WithDir("foo"),
			},
			check: func(t *testing.T, filename string) {
				assert.Check(t, strings.HasPrefix(filename, filepath.Join(os.TempDir(), "foo")))
			},
		},
		{
			name: "with perms",
			opts: []tempfile.Opts{
				tempfile.WithPerms(0o600),
			},
			check: func(t *testing.T, filename string) {
				if runtime.GOOS == "windows" {
					t.Skip("Windows doesn't support or need checking if chmod worked")
				}

				fi, err := os.Stat(filename)
				assert.NilError(t, err, "os.Stat(%q) = %s", filename, err)
				assert.Check(t, fi.Mode().Perm() == os.FileMode(0o600))
			},
		},
		{
			name: "with filename and keep extension",
			opts: []tempfile.Opts{
				tempfile.WithName("foo.txt"),
				tempfile.KeepingExtension(),
			},
			check: func(t *testing.T, filename string) {
				assert.Check(t, filepath.Ext(filename) == ".txt")
			},
		},
		{
			name: "without filename and keep extension",
			opts: []tempfile.Opts{
				tempfile.KeepingExtension(),
			},
			check: func(t *testing.T, filename string) {},
		},
	} {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			filename, err := tempfile.NewClosed(test.opts...)
			assert.NilError(t, err, `New(%v) = %v`, test.opts, err)

			t.Cleanup(func() {
				assert.Check(t, os.Remove(filename) == nil, "failed to remove file: %s", filename)
			})

			assert.Check(t, strings.HasPrefix(filename, os.TempDir()))
			test.check(t, filename)
		})
	}
}
