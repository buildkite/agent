package archive

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"

	"github.com/klauspost/compress/zip"
	"github.com/klauspost/compress/zstd"
)

// Experimental (A-1952): these let the cache client choose the ZIP entry
// method on the real save path, so benchmarks can compare Zstd and Store
// end to end. They are not a customer-facing setting. BuildArchive still
// always uses Zstd, and both methods produce archives that the normal
// extraction path reads.
//
// "zstd_parallel" writes the same Zstd ZIP entries (method 93) at the same
// level, but encodes each entry with klauspost's job-based parallel stream
// encoder (WithConcurrentBlocks). Decoding is unchanged.

// maxParallelZstdEncoders bounds the parallel encoder's goroutines. Each one
// buffers a job of 4× the 8 MiB default window (32 MiB) plus overlap and
// output, so the encoder's buffers stay in the low hundreds of MiB.
const maxParallelZstdEncoders = 4

func parseEntryMethod(name string) (entryMethod, zip.Compressor, error) {
	switch name {
	case "zstd":
		return entryMethodZstd, nil, nil
	case "zstd_parallel":
		return entryMethodZstd, parallelZstdCompressor(), nil
	case "store":
		return entryMethodStore, nil, nil
	default:
		return 0, nil, fmt.Errorf("unsupported archive entry method %q: want zstd, zstd_parallel or store", name)
	}
}

// parallelZstdCompressor matches quickzip's default Zstd compressor (level
// SpeedDefault, no CRC, entropy detection on) plus bounded concurrent
// blocks. Encoders are pooled, like quickzip's.
var parallelZstdCompressor = sync.OnceValue(func() zip.Compressor {
	concurrency := min(runtime.GOMAXPROCS(0), maxParallelZstdEncoders)
	pool := &sync.Pool{New: func() any {
		enc, err := zstd.NewWriter(nil,
			zstd.WithEncoderCRC(false),
			zstd.WithEncoderLevel(zstd.SpeedDefault),
			zstd.WithNoEntropyCompression(false),
			zstd.WithEncoderConcurrency(concurrency),
			zstd.WithConcurrentBlocks(true),
		)
		if err != nil {
			panic(fmt.Sprintf("parallel zstd encoder options: %v", err))
		}
		return enc
	}}
	return func(w io.Writer) (io.WriteCloser, error) {
		enc := pool.Get().(*zstd.Encoder)
		enc.Reset(w)
		return &pooledZstdEncoder{Encoder: enc, pool: pool}, nil
	}
})

type pooledZstdEncoder struct {
	*zstd.Encoder
	pool *sync.Pool
}

func (e *pooledZstdEncoder) Close() error {
	err := e.Encoder.Close()
	e.pool.Put(e.Encoder)
	return err
}

// ValidateEntryMethod returns an error unless name is "zstd",
// "zstd_parallel" or "store".
func ValidateEntryMethod(name string) error {
	_, _, err := parseEntryMethod(name)
	return err
}

// BuildArchiveWithMethod is BuildArchive with the entry method named by
// methodName ("zstd", "zstd_parallel" or "store"). Checksumming and archive
// layout are the same as BuildArchive's.
func BuildArchiveWithMethod(ctx context.Context, paths []string, key, methodName string) (*ArchiveInfo, error) {
	method, compressor, err := parseEntryMethod(methodName)
	if err != nil {
		return nil, err
	}
	return buildArchiveWithCompressor(ctx, paths, key, method, compressor)
}

// CheckEntryMethod returns an error unless every non-empty regular file in
// the archive uses the method named by methodName. Directories, symlinks,
// empty files (always stored) and the manifest don't depend on the method,
// so they are not checked. It returns the number of entries checked.
// "zstd_parallel" entries are ordinary Zstd entries, so this can't tell them
// apart from "zstd" ones.
func CheckEntryMethod(archiveFile string, archiveSize int64, methodName string) (int, error) {
	method, _, err := parseEntryMethod(methodName)
	if err != nil {
		return 0, err
	}

	f, err := os.Open(archiveFile)
	if err != nil {
		return 0, fmt.Errorf("failed to open archive file: %w", err)
	}
	defer func() { _ = f.Close() }()

	reader, err := zip.NewReader(f, archiveSize)
	if err != nil {
		return 0, fmt.Errorf("failed to open zip reader: %w", err)
	}

	checked := 0
	for _, entry := range reader.File {
		if entry.Name == ManifestPath || !entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 {
			continue
		}
		if entry.Method != uint16(method) {
			return checked, fmt.Errorf("archive entry %q uses ZIP method %d, want %d (%s)", entry.Name, entry.Method, method, methodName)
		}
		checked++
	}
	return checked, nil
}
