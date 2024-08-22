//go:build unix

package job

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestHardRemoveAll(t *testing.T) {
	container, err := os.MkdirTemp("", "TestHardRemoveAll")
	if err != nil {
		t.Fatalf("os.MkdirTemp(TestHardRemoveAll) error = %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(container) }) // lol but if hardRemoveAll doesn't work...

	dirA := filepath.Join(container, "a")
	dirB := filepath.Join(dirA, "b")
	fileC := filepath.Join(dirB, "c")
	if err := os.MkdirAll(dirB, 0o777); err != nil {
		t.Fatalf("os.MkdirAll(c%q, 0o777) = %v", dirB, err)
	}
	if err := os.WriteFile(fileC, []byte("hello!\n"), 0o664); err != nil {
		t.Fatalf("os.WriteFile(%q, hello!, 0o664) = %v", fileC, err)
	}

	// break directory perms
	if err := os.Chmod(dirB, 0o666); err != nil {
		t.Fatalf("os.Chmod(%q, 0o666) = %v", dirB, err)
	}
	if err := os.Chmod(dirA, 0o444); err != nil {
		t.Fatalf("os.Chmod(%q, 0o444) = %v", dirA, err)
	}

	if err := hardRemoveAll(dirA); err != nil {
		t.Errorf("hardRemoveAll(%q) = %v", dirA, err)
	}
	if _, err := os.Stat(dirA); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("os.Stat(%q) = %v, want %v", dirA, err, fs.ErrNotExist)
	}
}
