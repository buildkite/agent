package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/buildkite/agent/v3/logger"
)

func TestS3DowloaderBucketPath(t *testing.T) {
	t.Parallel()

	s3Downloader := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://my-bucket-name/foo/bar",
	})
	if got, want := s3Downloader.BucketPath(), "foo/bar"; got != want {
		t.Errorf("s3Downloader.BucketPath() = %q, want %q", got, want)
	}

	s3Downloader = NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://starts-with-an-s/and-this-is-its/folder",
	})
	if got, want := s3Downloader.BucketPath(), "and-this-is-its/folder"; got != want {
		t.Errorf("s3Downloader.BucketPath() = %q, want %q", got, want)
	}
}

func TestS3DowloaderBucketName(t *testing.T) {
	t.Parallel()

	s3Downloader := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://my-bucket-name/foo/bar",
	})
	if got, want := s3Downloader.BucketName(), "my-bucket-name"; got != want {
		t.Errorf("s3Downloader.BucketName() = %q, want %q", got, want)
	}

	s3Downloader = NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://starts-with-an-s",
	})
	if got, want := s3Downloader.BucketName(), "starts-with-an-s"; got != want {
		t.Errorf("s3Downloader.BucketName() = %q, want %q", got, want)
	}
}

func TestS3DowloaderBucketFileLocation(t *testing.T) {
	t.Parallel()

	s3Downloader := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://my-bucket-name/s3/folder",
		Path:   "here/please/right/now/",
	})
	if got, want := s3Downloader.BucketFileLocation(), "s3/folder/here/please/right/now/"; got != want {
		t.Errorf("s3Downloader.BucketFileLocation() = %q, want %q", got, want)
	}

	s3Downloader = NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://my-bucket-name/s3/folder",
		Path:   "",
	})
	if got, want := s3Downloader.BucketFileLocation(), "s3/folder/"; got != want {
		t.Errorf("s3Downloader.BucketFileLocation() = %q, want %q", got, want)
	}
}

// fakeS3Object is an httptest handler simulating S3 GetObject for a single
// object. It supports Range requests (used by the multipart path) and plain
// GETs (used by the single-stream path's presigned URL).
type fakeS3Object struct {
	content      []byte
	rangeReqs    atomic.Int64
	nonRangeReqs atomic.Int64
}

func (f *fakeS3Object) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	rangeHeader := req.Header.Get("Range")
	if rangeHeader == "" {
		f.nonRangeReqs.Add(1)
		rw.Header().Set("Content-Length", strconv.Itoa(len(f.content)))
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write(f.content)
		return
	}

	f.rangeReqs.Add(1)
	var start, end int64
	if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
		http.Error(rw, "bad range", http.StatusBadRequest)
		return
	}
	if start < 0 || end >= int64(len(f.content)) {
		http.Error(rw, "range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	rw.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(f.content)))
	rw.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	rw.WriteHeader(http.StatusPartialContent)
	_, _ = rw.Write(f.content[start : end+1])
}

func newTestS3Client(t *testing.T, server *httptest.Server) *s3.Client {
	t.Helper()
	return s3.New(s3.Options{
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		BaseEndpoint: aws.String(server.URL),
		UsePathStyle: true,
	})
}

// 25 MiB of deterministic content forces at least 4 ranged GETs at the 8 MiB
// part size, so multipart logic is actually exercised.
func multipartTestContent() []byte {
	return bytes.Repeat([]byte("A"), 25*1024*1024)
}

func TestS3Downloader_MultipartDownload_HappyPath(t *testing.T) {
	t.Parallel()

	content := multipartTestContent()
	wantHash := sha256.Sum256(content)
	wantHex := hex.EncodeToString(wantHash[:])

	fake := &fakeS3Object{content: content}
	server := httptest.NewServer(fake)
	defer server.Close()

	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "obj.bin")

	d := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Client:         newTestS3Client(t, server),
		S3Path:           "s3://test-bucket",
		Path:             "obj.bin",
		Destination:      tempDir,
		ExpectedSHA256:   wantHex,
		AllowS3Multipart: true,
	})

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("d.Start() = %v", err)
	}

	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) = %v", targetPath, err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("downloaded content mismatch: got %d bytes, want %d", len(got), len(content))
	}
	if got := fake.rangeReqs.Load(); got <= 1 {
		t.Errorf("rangeReqs = %d, want > 1 (multipart should issue multiple ranged GETs)", got)
	}
}

func TestS3Downloader_MultipartDownload_VerifyChecksumMismatch(t *testing.T) {
	t.Parallel()

	content := multipartTestContent()
	actualHash := sha256.Sum256(content)
	actualHex := hex.EncodeToString(actualHash[:])
	wrongHex := strings.Repeat("0", 64)

	fake := &fakeS3Object{content: content}
	server := httptest.NewServer(fake)
	defer server.Close()

	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "obj.bin")

	d := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Client:         newTestS3Client(t, server),
		S3Path:           "s3://test-bucket",
		Path:             "obj.bin",
		Destination:      tempDir,
		ExpectedSHA256:   wrongHex,
		AllowS3Multipart: true,
	})

	err := d.Start(context.Background())
	if err == nil {
		t.Fatal("d.Start() = nil, want checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum") || !strings.Contains(err.Error(), actualHex) {
		t.Errorf("d.Start() error = %v, want one containing 'checksum' and %q", err, actualHex)
	}

	if _, err := os.Stat(targetPath); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("target path %q exists after checksum mismatch (Stat err = %v)", targetPath, err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(tempDir, "obj.bin*"))
	if len(leftovers) != 0 {
		t.Errorf("found leftover files in temp dir: %v", leftovers)
	}
}

func TestS3Downloader_MultipartDownload_VerifyChecksumSkippedWhenAbsent(t *testing.T) {
	t.Parallel()

	content := multipartTestContent()

	fake := &fakeS3Object{content: content}
	server := httptest.NewServer(fake)
	defer server.Close()

	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "obj.bin")

	d := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Client:         newTestS3Client(t, server),
		S3Path:           "s3://test-bucket",
		Path:             "obj.bin",
		Destination:      tempDir,
		AllowS3Multipart: true,
		// ExpectedSHA256 deliberately empty — legacy artifacts have no digest.
	})

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("d.Start() = %v", err)
	}

	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) = %v", targetPath, err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("downloaded content mismatch: got %d bytes, want %d", len(got), len(content))
	}
}

func TestS3Downloader_DisableMultipartDispatchesSingleStream(t *testing.T) {
	t.Parallel()

	content := multipartTestContent()

	t.Run("disabled_uses_single_stream", func(t *testing.T) {
		t.Parallel()

		fake := &fakeS3Object{content: content}
		server := httptest.NewServer(fake)
		defer server.Close()

		d := NewS3Downloader(logger.Discard, S3DownloaderConfig{
			S3Client:         newTestS3Client(t, server),
			S3Path:           "s3://test-bucket",
			Path:             "obj.bin",
			Destination:      t.TempDir(),
			Retries:          1,
			AllowS3Multipart: false,
		})

		if err := d.Start(context.Background()); err != nil {
			t.Fatalf("d.Start() = %v", err)
		}

		if got := fake.nonRangeReqs.Load(); got != 1 {
			t.Errorf("nonRangeReqs = %d, want 1 (single-stream path should issue exactly one plain GET)", got)
		}
		if got := fake.rangeReqs.Load(); got != 0 {
			t.Errorf("rangeReqs = %d, want 0 (single-stream path should not issue ranged GETs)", got)
		}
	})

	t.Run("enabled_uses_ranges", func(t *testing.T) {
		t.Parallel()

		fake := &fakeS3Object{content: content}
		server := httptest.NewServer(fake)
		defer server.Close()

		d := NewS3Downloader(logger.Discard, S3DownloaderConfig{
			S3Client:         newTestS3Client(t, server),
			S3Path:           "s3://test-bucket",
			Path:             "obj.bin",
			Destination:      t.TempDir(),
			AllowS3Multipart: true,
		})

		if err := d.Start(context.Background()); err != nil {
			t.Fatalf("d.Start() = %v", err)
		}

		if got := fake.rangeReqs.Load(); got <= 1 {
			t.Errorf("rangeReqs = %d, want > 1 (multipart path)", got)
		}
		if got := fake.nonRangeReqs.Load(); got != 0 {
			t.Errorf("nonRangeReqs = %d, want 0 (multipart path should not issue plain GETs)", got)
		}
	})
}

func TestS3Downloader_MultipartDownload_CleansUpOnDownloadError(t *testing.T) {
	t.Parallel()

	// Server denies every request; SDK gives up without retries on 403.
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		http.Error(rw, "denied", http.StatusForbidden)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "obj.bin")

	d := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Client:         newTestS3Client(t, server),
		S3Path:           "s3://test-bucket",
		Path:             "obj.bin",
		Destination:      tempDir,
		AllowS3Multipart: false,
		Retries:          1,
	})

	if err := d.Start(context.Background()); err == nil {
		t.Fatal("d.Start() = nil, want download error")
	}

	if _, err := os.Stat(targetPath); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("target path %q exists after download error (Stat err = %v)", targetPath, err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(tempDir, "obj.bin*"))
	if len(leftovers) != 0 {
		t.Errorf("found leftover files in temp dir: %v", leftovers)
	}
}
