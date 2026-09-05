package job

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/osutil"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/tracetools"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
)

const cloneKitAnchor = "refs/buildkite-agent/origin-clonekit/anchor"

var (
	cloneKitRepoPath = regexp.MustCompile(`^/[A-Za-z0-9_][A-Za-z0-9_.-]*/[A-Za-z0-9_][A-Za-z0-9_.-]*\.git$`)
	cloneKitPackPath = regexp.MustCompile(`^objects/pack/pack-([0-9a-f]{40})\.(pack|idx|rev)$`)
	errCloneKitMiss  = errors.New("clonekit manifest unavailable")
)

func (e *Executor) originCloneKitSource(ctx context.Context, previousAttempts int) string {
	if !experiments.IsEnabled(ctx, experiments.OriginCloneKit) || previousAttempts != 0 ||
		e.GitMirrorsPath == "" || e.GitMirrorsSkipUpdate || e.GitCloneMirrorFlags != "" ||
		e.Repository != e.canonicalRepository || e.checkoutAlreadyExists() {
		return ""
	}
	source := e.GitRemoteMirrorURL
	if source == "" {
		source = e.Repository
	}
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "https" || u.Host != "origin.cursor.com" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || !cloneKitRepoPath.MatchString(u.Path) {
		return ""
	}
	return source
}

// tryOriginCloneKit owns only unpublished staging, under the mirror clone lock.
// All errors crossing this boundary are intentionally URL/credential-free.
func (e *Executor) tryOriginCloneKit(parent context.Context, source, repository, staging, destination string) (bool, error) {
	span, ctx := e.traceOpSpan(parent, "origin-clonekit")
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	started := time.Now()
	stage, outcome := "credentials", "error"
	sourceKind := "canonical"
	if source == e.GitRemoteMirrorURL {
		sourceKind = "remote-mirror"
	}
	defer func() {
		span.SetAttributes(attribute.String("outcome", outcome), attribute.String("stage", stage), attribute.String("source", sourceKind), attribute.Bool("fallback", outcome != "hit" && parent.Err() == nil), attribute.Float64("duration_seconds", time.Since(started).Seconds()))
		tracetools.FinishWithError(span, nil)
		e.shell.Commentf("Origin CloneKit: %s (%s)", outcome, stage)
	}()
	err := func() error {
		u, _ := url.Parse(source) // eligibility checked before acquiring the clone lock
		input := "protocol=https\nhost=" + u.Host + "\npath=" + strings.TrimPrefix(u.Path, "/") + "\n\n"
		args := append(remoteMirrorGitFlags(ctx), "credential", "fill")
		credentials, err := e.shell.CloneWithStdin(strings.NewReader(input)).Command("git", args...).RunAndCaptureStdout(ctx, shell.AlwaysHidePrompt(), shell.ShowStderr(false))
		if err != nil {
			return errors.New("credential lookup failed")
		}
		var username, password string
		for _, line := range strings.Split(credentials, "\n") {
			if v, ok := strings.CutPrefix(line, "username="); ok {
				username = v
			}
			if v, ok := strings.CutPrefix(line, "password="); ok {
				password = v
			}
		}
		if username == "" || password == "" || (username == "fail" && password == "fail") {
			return errors.New("credentials unavailable")
		}
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		stage = "manifest"
		t := time.Now()
		m, err := fetchCloneKitManifest(ctx, client, source+"/clone-kit", username, password)
		span.SetAttributes(attribute.Float64("manifest_seconds", time.Since(t).Seconds()))
		if err != nil {
			return err
		}
		stage = "prepare"
		if err := os.Mkdir(staging, 0o777); err != nil {
			return errors.New("create staging failed")
		}
		runGit := func(args ...string) error {
			_, err := e.shell.Command("git", args...).RunAndCaptureStdout(ctx, shell.AlwaysHidePrompt(), shell.ShowStderr(false))
			if err != nil {
				return errors.New("seed git operation failed")
			}
			return nil
		}
		if err := runGit("init", "--bare", "--object-format=sha1", "--initial-branch=main", staging); err != nil {
			return err
		}
		stage = "download"
		t = time.Now()
		g, downloadCtx := errgroup.WithContext(ctx)
		var bytes int64
		for _, file := range m.Files {
			bytes += file.Size
			if file.Name == "pack" {
				span.SetAttributes(attribute.Int("ranges", cloneKitRangeCount(file.Size)))
			}
			g.Go(func() error {
				return downloadCloneKitFile(downloadCtx, client, file, filepath.Join(staging, file.Name+".download"))
			})
		}
		if err := g.Wait(); err != nil {
			return err
		}
		span.SetAttributes(attribute.Float64("download_seconds", time.Since(t).Seconds()), attribute.Int64("bytes", bytes))
		stage = "verify"
		t = time.Now()
		for _, file := range m.Files {
			if err := verifyCloneKitFile(ctx, file, m.PackChunks, filepath.Join(staging, file.Name+".download")); err != nil {
				return err
			}
		}
		for _, file := range m.Files {
			// Paths have been validated, but derive destinations from the pack ID.
			name := "pack-" + m.packID + "." + file.Name
			// Mirror caches may be shared by agents running as different users.
			// Match Git's read-only pack permissions, respecting the host umask.
			if err := os.Chmod(filepath.Join(staging, file.Name+".download"), 0o444&^osutil.Umask); err != nil {
				return errors.New("set pack permissions failed")
			}
			if err := os.Rename(filepath.Join(staging, file.Name+".download"), filepath.Join(staging, "objects", "pack", name)); err != nil {
				return errors.New("install pack data failed")
			}
		}
		if err := runGit("--git-dir", staging, "cat-file", "-e", m.TipCommit+"^{commit}"); err != nil {
			return err
		}
		if err := runGit("--git-dir", staging, "fsck", "--connectivity-only", "--no-dangling", m.TipCommit); err != nil {
			return err
		}
		if err := runGit("--git-dir", staging, "update-ref", cloneKitAnchor, m.TipCommit); err != nil {
			return err
		}
		span.SetAttributes(attribute.Float64("verify_seconds", time.Since(t).Seconds()))
		stage = "publish"
		for _, config := range [][2]string{{"remote.origin.url", repository}, {"remote.origin.fetch", "+refs/*:refs/*"}, {"remote.origin.mirror", "true"}, {"remote.origin.tagOpt", "--no-tags"}} {
			if err := runGit("--git-dir", staging, "config", config[0], config[1]); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.Rename(staging, destination); err != nil {
			return errors.New("publish seed failed")
		}
		return nil
	}()
	if err == nil {
		outcome = "hit"
		return true, nil
	}
	if errors.Is(err, errCloneKitMiss) {
		outcome = "miss"
	}
	if ctx.Err() != nil {
		outcome = "timeout"
	}
	// Every worker and Git process has completed before staging can be removed.
	if cleanupErr := os.RemoveAll(staging); cleanupErr != nil {
		stage = "cleanup"
		// Canonical destination is independent. Leave interrupted staging for
		// next clone-lock acquisition and continue canonical checkout.
	}
	if parent.Err() != nil {
		outcome = "cancelled"
		return false, parent.Err()
	}
	return false, nil
}

type cloneKitFile struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type cloneKitChunks struct {
	ChunkSize int64    `json:"chunk_size"`
	SHA256s   []string `json:"sha256s"`
}

type cloneKitManifest struct {
	Version    int             `json:"version"`
	RepoID     string          `json:"repo_id"`
	TipCommit  string          `json:"tip_commit"`
	Files      []cloneKitFile  `json:"files"`
	PackChunks *cloneKitChunks `json:"pack_chunks"`
	packID     string
}

func fetchCloneKitManifest(ctx context.Context, client *http.Client, endpoint, username, password string) (*cloneKitManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("invalid manifest request")
	}
	req.SetBasicAuth(username, password)
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("manifest request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == 404 || resp.StatusCode == 410 {
		return nil, errCloneKitMiss
	}
	if resp.StatusCode != 200 {
		return nil, errors.New("manifest status rejected")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return nil, errors.New("manifest body rejected")
	}
	// encoding/json otherwise silently accepts duplicate keys (last value wins).
	if !json.Valid(body) || !cloneKitUniqueKeys(json.NewDecoder(bytes.NewReader(body)), 0) {
		return nil, errors.New("ambiguous manifest JSON")
	}
	var m cloneKitManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, errors.New("invalid manifest JSON")
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func cloneKitUniqueKeys(d *json.Decoder, depth int) bool {
	if depth > 64 {
		return false
	}
	token, err := d.Token()
	if err != nil {
		return false
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return true
	}
	keys := make(map[string]bool)
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return false
			}
			s, ok := key.(string)
			s = strings.ToLower(s) // encoding/json also matches struct fields case-insensitively
			if !ok || keys[s] {
				return false
			}
			keys[s] = true
		}
		if !cloneKitUniqueKeys(d, depth+1) {
			return false
		}
	}
	_, err = d.Token()
	return err == nil
}

func cloneKitDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && s == strings.ToLower(s)
}

func (m *cloneKitManifest) validate() error {
	invalid := errors.New("invalid clonekit manifest")
	id, err := uuid.Parse(m.RepoID)
	if err != nil || id == uuid.Nil || id.String() != m.RepoID || m.Version != 1 || !fullSHA1ObjectID.MatchString(m.TipCommit) || len(m.Files) != 3 {
		return invalid
	}
	seen := make(map[string]bool)
	var packSize int64
	for _, f := range m.Files {
		parts := cloneKitPackPath.FindStringSubmatch(f.Path)
		if len(parts) != 3 || parts[2] != f.Name || seen[f.Name] || !cloneKitDigest(f.SHA256) || f.Size <= 0 {
			return invalid
		}
		if m.packID != "" && m.packID != parts[1] {
			return invalid
		}
		m.packID = parts[1]
		seen[f.Name] = true
		cap := int64(4 << 30)
		if f.Name == "pack" {
			cap = 64 << 30
			packSize = f.Size
		}
		if f.Size > cap {
			return invalid
		}
		u, err := url.Parse(f.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			return invalid
		}
	}
	if c := m.PackChunks; c != nil {
		if c.ChunkSize <= 0 || c.ChunkSize > 64<<30 || int64(len(c.SHA256s)) != (packSize-1)/c.ChunkSize+1 {
			return invalid
		}
		for _, digest := range c.SHA256s {
			if !cloneKitDigest(digest) {
				return invalid
			}
		}
	}
	return nil
}

func cloneKitRangeCount(size int64) int {
	if size < 64<<20 {
		return 1
	}
	return int(min(int64(32), size/(8<<20)))
}

func downloadCloneKitFile(ctx context.Context, client *http.Client, file cloneKitFile, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return errors.New("create download failed")
	}
	defer func() { _ = f.Close() }() // error path; successful writes explicitly close below
	if err := f.Truncate(file.Size); err != nil {
		return errors.New("size download failed")
	}
	n := 1
	if file.Name == "pack" {
		n = cloneKitRangeCount(file.Size)
	}
	g, ctx := errgroup.WithContext(ctx)
	for i := range n {
		start, end := int64(i)*file.Size/int64(n), int64(i+1)*file.Size/int64(n)-1
		g.Go(func() error {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
			if err != nil {
				return errors.New("invalid artifact request")
			}
			req.Header.Set("Accept-Encoding", "identity")
			status := http.StatusOK
			if n > 1 {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
				status = http.StatusPartialContent
			}
			resp, err := client.Do(req)
			if err != nil {
				return errors.New("artifact request failed")
			}
			defer func() { _ = resp.Body.Close() }()
			length := end - start + 1
			if resp.StatusCode != status || resp.ContentLength != length || (resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity") {
				return errors.New("artifact response rejected")
			}
			if n > 1 && resp.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", start, end, file.Size) {
				return errors.New("artifact range rejected")
			}
			if _, err := io.CopyN(io.NewOffsetWriter(f, start), resp.Body, length); err != nil {
				return errors.New("artifact transfer failed")
			}
			var extra [1]byte
			if n, err := resp.Body.Read(extra[:]); n != 0 || err != io.EOF {
				return errors.New("artifact length rejected")
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return errors.New("close download failed")
	}
	return nil
}

func verifyCloneKitFile(ctx context.Context, file cloneKitFile, chunks *cloneKitChunks, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.New("open verification failed")
	}
	defer func() { _ = f.Close() }() // read-only handle
	size, hashes := file.Size, []string{file.SHA256}
	if file.Name == "pack" && chunks != nil {
		size, hashes = chunks.ChunkSize, chunks.SHA256s
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for i, digest := range hashes {
		g.Go(func() error {
			h := sha256.New()
			r := io.NewSectionReader(f, int64(i)*size, min(size, file.Size-int64(i)*size))
			buf := make([]byte, 64<<10)
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				n, err := r.Read(buf)
				h.Write(buf[:n])
				if err == io.EOF {
					break
				}
				if err != nil {
					return errors.New("read verification failed")
				}
			}
			if hex.EncodeToString(h.Sum(nil)) != digest {
				return errors.New("artifact digest mismatch")
			}
			return nil
		})
	}
	return g.Wait()
}
