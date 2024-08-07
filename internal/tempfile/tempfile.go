package tempfile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const allRWX = 0o777

type request struct {
	filename      string
	dir           string
	perm          fs.FileMode
	keepExtension bool
}

type Opts func(*request)

// WithName sets the filename of the temporary file.
func WithName(filename string) Opts {
	return func(tf *request) {
		tf.filename = filename
	}
}

// WithDir sets the subdirectory to contain the temporary file. If dir is
// absolute, it will be used as-is, otherwise it will be taken relative to the
// system temporary directory ([os.TempDir]).
func WithDir(dir string) Opts {
	return func(tf *request) {
		tf.dir = dir
	}
}

// WithPerms sets the permissions of the temporary file.
func WithPerms(perms fs.FileMode) Opts {
	return func(tf *request) {
		tf.perm = perms
	}
}

// KeepingExtension ensures the extension of the filename is preserved when creating the temporary
// file. It has not affect if `WithName` is not also used.
func KeepingExtension() Opts {
	return func(tf *request) {
		tf.keepExtension = true
	}
}

// New creates a temporary file with the provided options.
func New(opts ...Opts) (*os.File, error) {
	req := &request{}

	for _, opt := range opts {
		opt(req)
	}

	dir := os.TempDir()
	if req.dir != "" {
		if filepath.IsAbs(req.dir) {
			dir = filepath.Clean(req.dir)
		} else {
			dir = filepath.Join(dir, req.dir)
		}
	}

	// umask will make perms more reasonable
	if err := os.MkdirAll(dir, allRWX); err != nil {
		return nil, fmt.Errorf("failed to create temporary directory %q: %w", dir, err)
	}

	tempFileName := req.filename
	if req.keepExtension {
		extension := filepath.Ext(req.filename)
		basename := strings.TrimSuffix(req.filename, extension)
		tempFileName = basename + "-*" + extension
	}

	tempFile, err := os.CreateTemp(dir, tempFileName)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file %q: %w", req.filename, err)
	}

	if req.perm != 0 {
		if err := tempFile.Chmod(req.perm); err != nil {
			return nil, fmt.Errorf("failed to chmod temporary file %q: %w", tempFile.Name(), err)
		}
	}

	return tempFile, nil
}

// NewClosed creates a temporary file with the provided options and closes it.
func NewClosed(opts ...Opts) (string, error) {
	f, err := New(opts...)
	if err != nil {
		return "", err
	}
	return f.Name(), f.Close()
}
