package cache

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/buildkite/agent/v4/internal/cache/archive"
	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/klauspost/compress/zstd"
)

// Recorded output is a zstd stream of outputFormatHeader then one [stream byte][uvarint length][bytes] frame per write, so replay keeps streams and rough order.
const outputFormatHeader = "buildkite-agent cache exec output v1\n"

const (
	streamStdout byte = 1
	streamStderr byte = 2
)

// outputRecorder records redacted stdout/stderr to a temp file for upload (replaying jobs may not know our secrets); each stream may be written from one goroutine at a time.
type outputRecorder struct {
	stdout  *replacer.Replacer
	stderr  *replacer.Replacer
	mu      sync.Mutex
	file    *os.File
	sum     *archive.ChecksumSHA256
	enc     *zstd.Encoder
	written int64
	start   time.Time
	// err is the first recording failure; writes still succeed so recording never breaks the command, and Close returns it.
	err error
}

// newOutputRecorder starts a recording that redacts needles and Buildkite tokens.
func newOutputRecorder(needles []string) (*outputRecorder, error) {
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
	newRedactor := func(stream byte) *replacer.Replacer {
		repl := redact.New(streamWriter{r, stream}, needles)
		repl.AddPrefixes(redact.TokenPrefixes()...)
		return repl
	}
	r.stdout = newRedactor(streamStdout)
	r.stderr = newRedactor(streamStderr)
	return r, nil
}

// Path is the recording file. The caller removes it when done.
func (r *outputRecorder) Path() string { return r.file.Name() }

func (r *outputRecorder) Stdout() io.Writer { return r.stdout }
func (r *outputRecorder) Stderr() io.Writer { return r.stderr }

type streamWriter struct {
	r      *outputRecorder
	stream byte
}

func (w streamWriter) Write(p []byte) (int, error) {
	w.r.record(w.stream, p)
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
	r.written += int64(len(p))
}

// Close finishes the recording and describes it as a blob to upload; writes must have finished.
func (r *outputRecorder) Close() (*archive.ArchiveInfo, error) {
	// Flush output the redactors held back while matching a possible secret.
	flushErr := errors.Join(r.stdout.Flush(), r.stderr.Flush())
	r.mu.Lock()
	defer r.mu.Unlock()
	err := errors.Join(flushErr, r.err, r.enc.Close())
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

// reRedactOutput copies a recording through a new recorder that redacts needles, returning the new recording; the caller removes it.
func reRedactOutput(path string, needles []string) (*archive.ArchiveInfo, error) {
	rec, err := newOutputRecorder(needles)
	if err != nil {
		return nil, err
	}
	replayErr := replayOutput(path, rec.Stdout(), rec.Stderr())
	info, err := rec.Close()
	if err = errors.Join(replayErr, err); err != nil {
		_ = os.Remove(rec.Path())
		return nil, fmt.Errorf("failed to redact recorded output: %w", err)
	}
	return info, nil
}

// replayOutput writes a recording back to stdout and stderr; replaying to io.Discard validates it.
func replayOutput(path string, stdout, stderr io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	dec, err := zstd.NewReader(f)
	if err != nil {
		return err
	}
	defer dec.Close()
	br := bufio.NewReader(dec)

	header := make([]byte, len(outputFormatHeader))
	if _, err := io.ReadFull(br, header); err != nil || string(header) != outputFormatHeader {
		return errors.Join(errors.New("unrecognized output recording format"), err)
	}

	for {
		stream, err := br.ReadByte()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		n, err := binary.ReadUvarint(br)
		if err != nil {
			return fmt.Errorf("truncated output recording: %w", err)
		}
		var w io.Writer
		switch stream {
		case streamStdout:
			w = stdout
		case streamStderr:
			w = stderr
		default:
			return fmt.Errorf("unknown output stream %d in recording", stream)
		}
		if _, err := io.CopyN(w, br, int64(n)); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return fmt.Errorf("truncated output recording: %w", err)
		}
	}
}
