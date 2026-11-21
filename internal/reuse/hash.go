package reuse

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/zeebo/blake3"
)

// DigestPrefix prefixes every digest produced by this package.
const DigestPrefix = "b3:"

// ErrNoFilesMatched is returned by HashFiles when the patterns match no files.
var ErrNoFilesMatched = errors.New("inputs.files matched no files")

// FileEntry is one hashed input file.
type FileEntry struct {
	Path       string // forward-slash path relative to the repository directory
	Executable bool
	Leaf       string // hex BLAKE3 of the content
}

// Result is the result of HashFiles.
type Result struct {
	Digest string
	Count  int
	Files  []FileEntry
}

// indexEntry is a file listed by git.
type indexEntry struct {
	mode string // e.g. 100644, 100755, 120000, 160000; empty for untracked
	oid  string // blob (or commit, for gitlinks) id; empty for untracked
	path string
}

// Hasher hashes input files, caching git file listings per directory so that
// hashing several steps within one pipeline upload only lists files once.
type Hasher struct {
	tracked   map[string][]indexEntry
	untracked map[string][]indexEntry
}

// HashFiles hashes the committed content of the tracked files in repoDir
// whose paths match any include pattern and no exclude pattern. If untracked
// is true, untracked and ignored files are also considered, and read from the
// working tree.
func HashFiles(ctx context.Context, repoDir string, include, exclude []string, untracked bool) (Result, error) {
	return new(Hasher).HashFiles(ctx, repoDir, include, exclude, untracked)
}

// HashFiles is like the package-level HashFiles, but reuses cached listings.
func (h *Hasher) HashFiles(ctx context.Context, repoDir string, include, exclude []string, untracked bool) (Result, error) {
	incl, err := CompilePatterns(include)
	if err != nil {
		return Result{}, fmt.Errorf("include patterns: %w", err)
	}
	excl, err := CompilePatterns(exclude)
	if err != nil {
		return Result{}, fmt.Errorf("exclude patterns: %w", err)
	}

	tracked, err := h.listTracked(ctx, repoDir)
	if err != nil {
		return Result{}, err
	}

	var matched []indexEntry
	seen := make(map[string]bool)
	for _, e := range tracked {
		if seen[e.path] || !matches(e.path, incl, excl) {
			continue
		}
		seen[e.path] = true
		matched = append(matched, e)
	}

	if untracked {
		others, err := h.listUntracked(ctx, repoDir)
		if err != nil {
			return Result{}, err
		}
		for _, e := range others {
			if seen[e.path] || !matches(e.path, incl, excl) {
				continue
			}
			seen[e.path] = true
			matched = append(matched, e)
		}
	}

	if len(matched) == 0 {
		return Result{}, ErrNoFilesMatched
	}

	files, err := leaves(ctx, repoDir, matched)
	if err != nil {
		return Result{}, err
	}

	slices.SortFunc(files, func(a, b FileEntry) int { return strings.Compare(a.Path, b.Path) })

	digest := blake3.New()
	for _, f := range files {
		x := "-"
		if f.Executable {
			x = "x"
		}
		_, _ = io.WriteString(digest, f.Path+"\x00"+x+"\x00"+f.Leaf+"\n")
	}

	return Result{
		Digest: DigestPrefix + hex.EncodeToString(digest.Sum(nil)),
		Count:  len(files),
		Files:  files,
	}, nil
}

func (h *Hasher) listTracked(ctx context.Context, repoDir string) ([]indexEntry, error) {
	if entries, ok := h.tracked[repoDir]; ok {
		return entries, nil
	}
	out, err := gitOutput(ctx, repoDir, "ls-files", "-s", "-z")
	if err != nil {
		return nil, err
	}
	var entries []indexEntry
	for rec := range strings.SplitSeq(string(out), "\x00") {
		if rec == "" {
			continue
		}
		// <mode> SP <object> SP <stage> TAB <path>
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok {
			return nil, fmt.Errorf("unexpected git ls-files output %q", rec)
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			return nil, fmt.Errorf("unexpected git ls-files output %q", rec)
		}
		entries = append(entries, indexEntry{mode: fields[0], oid: fields[1], path: path})
	}
	if h.tracked == nil {
		h.tracked = make(map[string][]indexEntry)
	}
	h.tracked[repoDir] = entries
	return entries, nil
}

func (h *Hasher) listUntracked(ctx context.Context, repoDir string) ([]indexEntry, error) {
	if entries, ok := h.untracked[repoDir]; ok {
		return entries, nil
	}
	out, err := gitOutput(ctx, repoDir, "ls-files", "-o", "-z")
	if err != nil {
		return nil, err
	}
	var entries []indexEntry
	for path := range strings.SplitSeq(string(out), "\x00") {
		if path == "" {
			continue
		}
		entries = append(entries, indexEntry{path: path})
	}
	if h.untracked == nil {
		h.untracked = make(map[string][]indexEntry)
	}
	h.untracked[repoDir] = entries
	return entries, nil
}

func gitOutput(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// leaves computes the leaf hash of each entry. Tracked blobs are read with a
// single `git cat-file --batch` process; untracked files are read from disk.
func leaves(ctx context.Context, repoDir string, entries []indexEntry) ([]FileEntry, error) {
	files := make([]FileEntry, 0, len(entries))

	var batch *catFile
	defer func() {
		if batch != nil {
			_ = batch.close()
		}
	}()

	for _, e := range entries {
		switch e.mode {
		case "":
			f, err := untrackedLeaf(repoDir, e.path)
			if err != nil {
				return nil, err
			}
			if f != nil {
				files = append(files, *f)
			}

		case "160000":
			// Submodule: the commit id identifies the content.
			files = append(files, FileEntry{Path: e.path, Leaf: leafOf([]byte(e.oid))})

		default:
			if batch == nil {
				b, err := newCatFile(ctx, repoDir)
				if err != nil {
					return nil, err
				}
				batch = b
			}
			h := blake3.New()
			if err := batch.blob(e.oid, h); err != nil {
				return nil, fmt.Errorf("reading %s (%s): %w", e.path, e.oid, err)
			}
			files = append(files, FileEntry{
				Path:       e.path,
				Executable: e.mode == "100755",
				Leaf:       hex.EncodeToString(h.Sum(nil)),
			})
		}
	}
	return files, nil
}

// untrackedLeaf hashes an untracked file from disk. It returns nil for
// anything that is neither a regular file nor a symlink.
func untrackedLeaf(repoDir, path string) (*FileEntry, error) {
	full := filepath.Join(repoDir, filepath.FromSlash(path))
	fi, err := os.Lstat(full)
	if err != nil {
		return nil, err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(full)
		if err != nil {
			return nil, err
		}
		return &FileEntry{Path: path, Leaf: leafOf([]byte(filepath.ToSlash(target)))}, nil

	case fi.Mode().IsRegular():
		leaf, err := fileLeaf(full)
		if err != nil {
			return nil, err
		}
		return &FileEntry{Path: path, Executable: fi.Mode()&0o111 != 0, Leaf: leaf}, nil
	}
	return nil, nil
}

func leafOf(b []byte) string {
	sum := blake3.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func fileLeaf(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := blake3.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// catFile is a long-running `git cat-file --batch` process.
type catFile struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func newCatFile(ctx context.Context, repoDir string) (*catFile, error) {
	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch")
	cmd.Dir = repoDir
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting git cat-file --batch: %w", err)
	}
	return &catFile{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}

// blob writes the content of the object oid to w.
func (c *catFile) blob(oid string, w io.Writer) error {
	if _, err := io.WriteString(c.in, oid+"\n"); err != nil {
		return err
	}
	header, err := c.out.ReadString('\n')
	if err != nil {
		return err
	}
	// <oid> SP <type> SP <size> LF, or <oid> SP missing LF
	fields := strings.Fields(header)
	if len(fields) != 3 {
		return fmt.Errorf("git cat-file: %s", strings.TrimSpace(header))
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return fmt.Errorf("git cat-file: bad size in %q", strings.TrimSpace(header))
	}
	if _, err := io.CopyN(w, c.out, size); err != nil {
		return err
	}
	// Trailing LF after the content.
	_, err = c.out.ReadByte()
	return err
}

func (c *catFile) close() error {
	_ = c.in.Close()
	return c.cmd.Wait()
}
