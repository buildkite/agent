package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/cache/internal/trace"
)

const (
	defaultBenchmarkFixtureSize = int64(256 << 20)
	benchmarkFileSize           = int64(4 << 20)
)

// BenchmarkCacheArchive compares Zstd-compressed cache archives with
// uncompressed cache archives.
// It uses a deterministic fixture split evenly between compressible and
// pseudo-random files. Transfer phases are local file copies, isolating their
// byte and CPU costs rather than pretending to model a particular network.
//
// Run the practical 256 MiB fixture once per method with:
//
//	go test ./internal/cache/archive -run '^$' -bench '^BenchmarkCacheArchive$' -benchtime=1x
//
// For a 10 GiB investigation trial, set BUILDKITE_CACHE_BENCHMARK_GIB=10 and
// keep -benchtime=1x. Ensure the temporary filesystem has ample free space.
func BenchmarkCacheArchive(b *testing.B) {
	if _, err := trace.NewProvider(b.Context(), "noop", "benchmark", "0.0.1"); err != nil {
		b.Fatalf("trace.NewProvider: %v", err)
	}

	fixtureSize := benchmarkFixtureSize(b)
	root := b.TempDir()
	cacheDir := filepath.Join(root, "cache")
	writeBenchmarkFixture(b, cacheDir, fixtureSize)

	for _, method := range []struct {
		name string
		id   entryMethod
	}{
		{name: "zstd", id: entryMethodZstd},
		{name: "store", id: entryMethodStore},
	} {
		b.Run(method.name, func(b *testing.B) {
			var archiveTime, uploadTime, downloadTime time.Duration
			var digestTime, extractTime, saveTime, restoreTime time.Duration
			var archiveSize int64

			remotePath := filepath.Join(root, method.name+"-remote.zip")
			downloadPath := filepath.Join(root, method.name+"-download.zip")

			b.ResetTimer()
			for range b.N {
				start := time.Now()
				info, err := buildArchive(b.Context(), []string{cacheDir}, "benchmark-"+method.name, method.id)
				archiveDuration := time.Since(start)
				if err != nil {
					b.Fatalf("buildArchive: %v", err)
				}
				archiveSize = info.Size

				start = time.Now()
				if err := copyBenchmarkFile(info.ArchivePath, remotePath); err != nil {
					b.Fatalf("upload copy: %v", err)
				}
				uploadDuration := time.Since(start)

				if err := os.RemoveAll(cacheDir); err != nil {
					b.Fatalf("RemoveAll fixture: %v", err)
				}

				start = time.Now()
				if err := copyBenchmarkFile(remotePath, downloadPath); err != nil {
					b.Fatalf("download copy: %v", err)
				}
				downloadDuration := time.Since(start)

				start = time.Now()
				gotDigest, err := benchmarkFileSHA256(downloadPath)
				digestDuration := time.Since(start)
				if err != nil {
					b.Fatalf("digest downloaded archive: %v", err)
				}
				if gotDigest != info.Sha256sum {
					b.Fatalf("download digest = %s, want %s", gotDigest, info.Sha256sum)
				}

				start = time.Now()
				downloaded, err := os.Open(downloadPath)
				if err == nil {
					_, err = ExtractFiles(b.Context(), downloaded, info.Size, []string{cacheDir})
					if closeErr := downloaded.Close(); err == nil {
						err = closeErr
					}
				}
				extractDuration := time.Since(start)
				if err != nil {
					b.Fatalf("ExtractFiles: %v", err)
				}
				if got := benchmarkTreeSize(b, cacheDir); got != fixtureSize {
					b.Fatalf("restored fixture size = %d, want %d", got, fixtureSize)
				}

				archiveTime += archiveDuration
				uploadTime += uploadDuration
				downloadTime += downloadDuration
				digestTime += digestDuration
				extractTime += extractDuration
				saveTime += archiveDuration + uploadDuration
				restoreTime += downloadDuration + digestDuration + extractDuration

				if err := os.Remove(info.ArchivePath); err != nil {
					b.Fatalf("remove archive: %v", err)
				}
			}
			b.StopTimer()

			n := float64(b.N)
			b.ReportMetric(float64(fixtureSize), "source-B")
			b.ReportMetric(float64(archiveSize), "archive-B")
			b.ReportMetric(float64(archiveSize)/float64(fixtureSize), "archive/source")
			b.ReportMetric(float64(fixtureSize)/float64(archiveSize), "source/archive")
			b.ReportMetric(float64(fixtureSize-archiveSize), "network-saved-B")
			reportBenchmarkPhase(b, "archive", archiveTime, fixtureSize, n)
			reportBenchmarkPhase(b, "upload", uploadTime, archiveSize, n)
			reportBenchmarkPhase(b, "download", downloadTime, archiveSize, n)
			reportBenchmarkPhase(b, "digest", digestTime, archiveSize, n)
			reportBenchmarkPhase(b, "extract", extractTime, fixtureSize, n)
			b.ReportMetric(float64(saveTime.Nanoseconds())/n, "save-ns/op")
			b.ReportMetric(float64(restoreTime.Nanoseconds())/n, "restore-ns/op")
		})
	}
}

func benchmarkFixtureSize(b *testing.B) int64 {
	b.Helper()
	value := os.Getenv("BUILDKITE_CACHE_BENCHMARK_GIB")
	if value == "" {
		return defaultBenchmarkFixtureSize
	}
	gib, err := strconv.ParseUint(value, 10, 31)
	if err != nil || gib == 0 {
		b.Fatalf("BUILDKITE_CACHE_BENCHMARK_GIB must be a positive integer, got %q", value)
	}
	return int64(gib) << 30
}

func writeBenchmarkFixture(b *testing.B, dir string, size int64) {
	b.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.Fatalf("MkdirAll fixture: %v", err)
	}

	buf := make([]byte, benchmarkFileSize)
	var written int64
	for fileIndex := 0; written < size; fileIndex++ {
		fileSize := min(int64(len(buf)), size-written)
		if fileIndex%2 == 0 {
			for i := range buf[:fileSize] {
				buf[i] = "buildkite-cache-fixture\n"[i%24]
			}
		} else {
			state := uint64(fileIndex + 1)
			for i := range buf[:fileSize] {
				state ^= state << 13
				state ^= state >> 7
				state ^= state << 17
				buf[i] = byte(state)
			}
		}
		name := filepath.Join(dir, fmt.Sprintf("group-%02d", fileIndex%16), fmt.Sprintf("cache-%06d.bin", fileIndex))
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			b.Fatalf("MkdirAll fixture group: %v", err)
		}
		if err := os.WriteFile(name, buf[:fileSize], 0o600); err != nil {
			b.Fatalf("WriteFile fixture: %v", err)
		}
		written += fileSize
	}
}

func copyBenchmarkFile(source, destination string) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}

func benchmarkFileSHA256(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func benchmarkTreeSize(b *testing.B, root string) int64 {
	b.Helper()
	var size int64
	if err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			size += info.Size()
		}
		return err
	}); err != nil {
		b.Fatalf("Walk restored fixture: %v", err)
	}
	return size
}

func reportBenchmarkPhase(b *testing.B, name string, duration time.Duration, bytes int64, iterations float64) {
	b.Helper()
	secondsPerIteration := duration.Seconds() / iterations
	b.ReportMetric(float64(duration.Nanoseconds())/iterations, name+"-ns/op")
	b.ReportMetric(float64(bytes)/(1_000_000*secondsPerIteration), name+"-MB/s")
}
