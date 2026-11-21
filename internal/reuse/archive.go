package reuse

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Pack writes root (a directory or a single file or symlink) to destTarGz as
// a gzipped tar archive, preserving file modes and symlinks. Entries within a
// directory are relative to it; a single file is stored under its base name.
func Pack(root, destTarGz string) (err error) {
	out, err := os.Create(destTarGz)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	fi, err := os.Lstat(root)
	if err != nil {
		return err
	}

	if fi.IsDir() {
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == root {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			return addEntry(tw, filepath.ToSlash(rel), p, info)
		})
	} else {
		err = addEntry(tw, filepath.Base(root), root, fi)
	}
	if err != nil {
		return err
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func addEntry(tw *tar.Writer, name, p string, info fs.FileInfo) error {
	var link string
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(p)
		if err != nil {
			return err
		}
		link = target
	case info.IsDir(), info.Mode().IsRegular():
	default:
		// Sockets, devices and the like are not outputs.
		return nil
	}

	hdr, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}
	hdr.Name = name
	if info.IsDir() {
		hdr.Name += "/"
	}
	// Ownership is not meaningful across agents.
	hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = 0, 0, "", ""

	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(tw, f)
	return err
}

// Unpack extracts a gzipped tar archive written by Pack into destDir,
// restoring file modes and symlinks. Entries with absolute paths or `..`
// components are rejected, and extraction cannot escape destDir.
func Unpack(srcTarGz, destDir string) error {
	in, err := os.Open(srcTarGz)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	gz, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(destDir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	// Directory modes are applied last, so that read-only directories can
	// still be filled.
	type dirMode struct {
		name string
		mode fs.FileMode
	}
	var dirModes []dirMode

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			for i := len(dirModes) - 1; i >= 0; i-- {
				if err := root.Chmod(dirModes[i].name, dirModes[i].mode); err != nil {
					return err
				}
			}
			return nil
		}
		if err != nil {
			return err
		}

		name, err := safeName(hdr.Name)
		if err != nil {
			return err
		}
		if name == "." {
			continue
		}
		if dir := path.Dir(name); dir != "." {
			if err := root.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}

		mode := fs.FileMode(hdr.Mode).Perm()
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o755); err != nil {
				return err
			}
			dirModes = append(dirModes, dirMode{name, mode})

		case tar.TypeSymlink:
			if err := root.Symlink(hdr.Linkname, name); err != nil {
				return err
			}

		case tar.TypeReg:
			f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
			// Apply the mode exactly, regardless of umask.
			if err := root.Chmod(name, mode); err != nil {
				return err
			}

		default:
			return fmt.Errorf("unsupported archive entry %q (type %c)", hdr.Name, hdr.Typeflag)
		}
	}
}

// safeName validates an archive entry name and returns it cleaned.
func safeName(name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || filepath.IsAbs(name) {
		return "", fmt.Errorf("unsafe archive entry %q", name)
	}
	for part := range strings.SplitSeq(strings.TrimSuffix(name, "/"), "/") {
		if part == ".." {
			return "", fmt.Errorf("unsafe archive entry %q", name)
		}
	}
	return path.Clean(name), nil
}
