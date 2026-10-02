package cache

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/buildkite/agent/v4/internal/cache/archive"
	"github.com/buildkite/agent/v4/jobapi"
	"github.com/dustin/go-humanize"
	"github.com/klauspost/compress/zstd"
)

// Recorded output is a zstd stream of outputFormatHeader then one [stream byte][uvarint length][bytes] frame per write, so replay keeps streams and rough order; a streamRanFor frame holds how long the command ran, in milliseconds.
const outputFormatHeader = "buildkite-agent cache exec output v2\n"

const (
	streamStdout byte = 1
	streamStderr byte = 2
	streamRanFor byte = 3
)

// maxRecordedOutput caps the output cache exec saves; a var so tests can lower it.
var maxRecordedOutput int64 = 10 << 20

// Redactor returns output chunks with secrets redacted, for output that's saved and replayed in other jobs.
type Redactor func(ctx context.Context, chunks []jobapi.OutputChunk) ([]jobapi.OutputChunk, error)

// outputRecorder records raw stdout/stderr to a temp file, up to maxRecordedOutput; each stream may be written from one goroutine at a time.
type outputRecorder struct {
	mu      sync.Mutex
	file    *os.File
	sum     *archive.ChecksumSHA256
	enc     *zstd.Encoder
	written int64
	start   time.Time
	// err is the first recording failure; writes still succeed so recording never breaks the command, and Close returns it.
	err error
}

func newOutputRecorder() (*outputRecorder, error) {
	f, err := os.CreateTemp("", "cache-exec-output-*.zst")
	if err != nil {
		return nil, fmt.Errorf("failed to create output recording file: %w", err)
	}
	sum := archive.NewChecksumSHA256(f)
	enc, err := zstd.NewWriter(sum)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, fmt.Errorf("failed to create output encoder: %w", err)
	}
	r := &outputRecorder{file: f, sum: sum, enc: enc, start: time.Now()}
	_, r.err = io.WriteString(enc, outputFormatHeader)
	return r, nil
}

// Path is the recording file. The caller removes it when done.
func (r *outputRecorder) Path() string { return r.file.Name() }

func (r *outputRecorder) Stdout() io.Writer { return streamWriter{r, streamStdout} }
func (r *outputRecorder) Stderr() io.Writer { return streamWriter{r, streamStderr} }

type streamWriter struct {
	r      *outputRecorder
	stream byte
}

func (w streamWriter) Write(p []byte) (int, error) {
	w.r.mu.Lock()
	tooLarge := w.r.written+int64(len(p)) > maxRecordedOutput
	if tooLarge && w.r.err == nil {
		w.r.err = fmt.Errorf("command output is larger than %s", humanize.IBytes(uint64(maxRecordedOutput)))
	}
	w.r.mu.Unlock()
	if !tooLarge {
		w.r.record(w.stream, p)
	}
	return len(p), nil
}

func (r *outputRecorder) record(stream byte, p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil || len(p) == 0 {
		return
	}
	frame := binary.AppendUvarint([]byte{stream}, uint64(len(p)))
	if _, err := r.enc.Write(frame); err != nil {
		r.err = err
		return
	}
	if _, err := r.enc.Write(p); err != nil {
		r.err = err
		return
	}
	if stream != streamRanFor {
		r.written += int64(len(p))
	}
}

// Close records how long the command ran, finishes the recording and describes it as a blob to upload; writes must have finished.
func (r *outputRecorder) Close(ranFor time.Duration) (*archive.ArchiveInfo, error) {
	r.record(streamRanFor, binary.AppendUvarint(nil, uint64(ranFor.Milliseconds())))
	r.mu.Lock()
	defer r.mu.Unlock()
	err := errors.Join(r.err, r.enc.Close())
	info, statErr := r.file.Stat()
	err = errors.Join(err, statErr, r.file.Close())
	if err != nil {
		return nil, fmt.Errorf("failed to record command output: %w", err)
	}
	return &archive.ArchiveInfo{
		ArchivePath:    r.file.Name(),
		Sha256sum:      r.sum.Sum(),
		Size:           info.Size(),
		WrittenBytes:   r.written,
		WrittenEntries: 1,
		Duration:       time.Since(r.start),
	}, nil
}

// redactRecording writes a redacted copy of a raw recording, returning the copy to upload; the caller removes it.
func redactRecording(ctx context.Context, path string, redact Redactor) (*archive.ArchiveInfo, error) {
	if redact == nil {
		return nil, errors.New("no way to redact secrets from the command output")
	}
	var chunks []jobapi.OutputChunk
	ranFor, err := replayOutput(path, jobapi.ChunkWriter{Chunks: &chunks}, jobapi.ChunkWriter{Chunks: &chunks, Stderr: true})
	if err != nil {
		return nil, fmt.Errorf("failed to read recorded output: %w", err)
	}
	if chunks, err = redact(ctx, chunks); err != nil {
		return nil, fmt.Errorf("failed to redact secrets from the command output: %w", err)
	}
	rec, err := newOutputRecorder()
	if err != nil {
		return nil, err
	}
	for _, c := range chunks {
		stream := streamStdout
		if c.Stderr {
			stream = streamStderr
		}
		rec.record(stream, c.Data)
	}
	info, err := rec.Close(ranFor)
	if err != nil {
		_ = os.Remove(rec.Path())
		return nil, err
	}
	return info, nil
}

// replayOutput writes a recording back to stdout and stderr and returns how long the command ran; replaying to io.Discard validates it.
func replayOutput(path string, stdout, stderr io.Writer) (ranFor time.Duration, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	dec, err := zstd.NewReader(f)
	if err != nil {
		return 0, err
	}
	defer dec.Close()
	br := bufio.NewReader(dec)

	header := make([]byte, len(outputFormatHeader))
	if _, err := io.ReadFull(br, header); err != nil || string(header) != outputFormatHeader {
		return 0, errors.Join(errors.New("unrecognized output recording format"), err)
	}

	for {
		stream, err := br.ReadByte()
		if err == io.EOF {
			return ranFor, nil
		}
		if err != nil {
			return 0, err
		}
		n, err := binary.ReadUvarint(br)
		if err != nil {
			return 0, fmt.Errorf("truncated output recording: %w", err)
		}
		if stream == streamRanFor {
			if n > binary.MaxVarintLen64 {
				return 0, fmt.Errorf("invalid run time frame in output recording")
			}
			buf := make([]byte, n)
			if _, err := io.ReadFull(br, buf); err != nil {
				return 0, fmt.Errorf("truncated output recording: %w", err)
			}
			ms, _ := binary.Uvarint(buf)
			ranFor = time.Duration(ms) * time.Millisecond
			continue
		}
		var w io.Writer
		switch stream {
		case streamStdout:
			w = stdout
		case streamStderr:
			w = stderr
		default:
			return 0, fmt.Errorf("unknown output stream %d in recording", stream)
		}
		if _, err := io.CopyN(w, br, int64(n)); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return 0, fmt.Errorf("truncated output recording: %w", err)
		}
	}
}
