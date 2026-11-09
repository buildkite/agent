package process_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/buildkite/agent/v4/internal/process"
	"github.com/google/go-cmp/cmp"
)

func TestProgramStatusWriter(t *testing.T) {
	t.Parallel()

	const (
		// msg=Compiling
		compiling40 = "\x1b]7501;state=working:id=build:progress=40:app=cargo:msg=Q29tcGlsaW5n\x1b\\"
		compiling60 = "\x1b]7501;state=working:id=build:progress=60:app=cargo:msg=Q29tcGlsaW5n\x07"
		// msg=Linking
		linking = "\x1b]7501;state=working:id=build:app=cargo:msg=TGlua2luZw==\x07"
		// title=Tests
		testsDone = "\x1b]7501;state=done:id=build/test:title=VGVzdHM=\x1b\\"
		noState   = "\x1b]7501;id=build:msg=TGlua2luZw==\x1b\\"
	)

	// Reports are written back without msg and title, always ending in ESC \.
	const (
		compiling40Out = "\x1b]7501;state=working:id=build:progress=40:app=cargo\x1b\\"
		compiling60Out = "\x1b]7501;state=working:id=build:progress=60:app=cargo\x1b\\"
		linkingOut     = "\x1b]7501;state=working:id=build:app=cargo\x1b\\"
		testsDoneOut   = "\x1b]7501;state=done:id=build/test\x1b\\"
	)

	var out bytes.Buffer
	w := process.NewProgramStatusWriter(&out)

	for _, chunk := range []string{
		// A report split across writes, arriving mid-line.
		"progress: 40%", compiling40[:9], compiling40[9:],
		// Only progress changed: no line.
		"\r\n", compiling60,
		// At the start of a line, after a colour code: no extra newline.
		"\x1b[32m", linking,
		"\x1b[0m", noState, testsDone,
	} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatalf("w.Write(%q) error = %v", chunk, err)
		}
	}

	want := "progress: 40%" + compiling40Out +
		"\n\x1b[90m# Program status: state=working id=build progress=40 app=cargo msg=\"Compiling\"\x1b[0m\n" +
		"\r\n" + compiling60Out +
		"\x1b[32m" + linkingOut +
		"\x1b[90m# Program status: state=working id=build app=cargo msg=\"Linking\"\x1b[0m\n" +
		"\x1b[0m" + testsDoneOut +
		"\x1b[90m# Program status: state=done id=build/test title=\"Tests\"\x1b[0m\n"
	if diff := cmp.Diff(want, out.String()); diff != "" {
		t.Errorf("output diff (-want +got):\n%s", diff)
	}
}

func TestProgramStatusWriterRepeatedClear(t *testing.T) {
	t.Parallel()

	const (
		clearAll = "\x1b]7501;state=clear\x07"
		building = "\x1b]7501;state=working:id=build\x07"
	)

	var out bytes.Buffer
	w := process.NewProgramStatusWriter(&out)
	for _, chunk := range []string{clearAll, building, clearAll, building} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatalf("w.Write(%q) error = %v", chunk, err)
		}
	}

	// The second clear repeats the first, so it gets no line, but it still
	// clears build, so the build report after it does.
	const (
		clearLine    = "\x1b[90m# Program status: state=clear\x1b[0m\n"
		buildingLine = "\x1b[90m# Program status: state=working id=build\x1b[0m\n"
	)
	const (
		clearAllOut = "\x1b]7501;state=clear\x1b\\"
		buildingOut = "\x1b]7501;state=working:id=build\x1b\\"
	)
	want := clearAllOut + clearLine + buildingOut + buildingLine + clearAllOut + buildingOut + buildingLine
	if diff := cmp.Diff(want, out.String()); diff != "" {
		t.Errorf("output diff (-want +got):\n%s", diff)
	}
}

func TestProgramStatusWriterFlush(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	w := process.NewProgramStatusWriter(&out)

	// An unterminated sequence is held back until Flush releases it unchanged.
	const partial = "\x1b]7501;state=working:msg=QQ=="
	if _, err := w.Write([]byte("before" + partial)); err != nil {
		t.Fatalf("w.Write() error = %v", err)
	}
	if got, want := out.String(), "before"; got != want {
		t.Errorf("output before Flush = %q, want %q", got, want)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("w.Flush() error = %v", err)
	}
	if got, want := out.String(), "before"+partial; got != want {
		t.Errorf("output after Flush = %q, want %q", got, want)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("second w.Flush() error = %v", err)
	}
	if got, want := out.String(), "before"+partial; got != want {
		t.Errorf("output after second Flush = %q, want %q", got, want)
	}
}

// After Flush, output from the next process starts afresh: it doesn't
// complete a sequence the previous process left unfinished.
func TestProgramStatusWriterFlushResets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		before string // ends partway into a sequence
		after  string
	}{
		{"unterminated sequence", "a\x1b]7501;state=wor", "king\a\n"},
		{"lone esc", "a\x1b", "]7501;state=done:msg=c2VjcmV0\a\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			w := process.NewProgramStatusWriter(&out)
			if _, err := w.Write([]byte(tt.before)); err != nil {
				t.Fatalf("w.Write(%q) error = %v", tt.before, err)
			}
			if err := w.Flush(); err != nil {
				t.Fatalf("w.Flush() error = %v", err)
			}
			if _, err := w.Write([]byte(tt.after)); err != nil {
				t.Fatalf("w.Write(%q) error = %v", tt.after, err)
			}
			if got, want := out.String(), tt.before+tt.after; got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestProgramStatusWriterMidLine(t *testing.T) {
	t.Parallel()

	const (
		report = "\x1b]7501;state=done:id=a:msg=QQ==\x07"
		out    = "\x1b]7501;state=done:id=a\x1b\\"
		line   = "\x1b[90m# Program status: state=done id=a msg=\"A\"\x1b[0m\n"
	)

	tests := []struct {
		name, input, want string
	}{
		{"mid-line", "text" + report, "text" + out + "\n" + line},
		{"line start", "text\n" + report, "text\n" + out + line},
		{"after re-encoded report", "text" + report + "more" + report[:0], "text" + out + "\n" + line + "more"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			w := process.NewProgramStatusWriter(&buf)
			if _, err := w.Write([]byte(test.input)); err != nil {
				t.Fatalf("w.Write() error = %v", err)
			}
			if diff := cmp.Diff(test.want, buf.String()); diff != "" {
				t.Errorf("output diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestProgramStatusWriterLineError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("disk full")
	w := process.NewProgramStatusWriter(failingLineWriter{err: wantErr})

	// The report is the last output, so only this Write can return the error.
	if _, err := w.Write([]byte("\x1b]7501;state=done\x07")); !errors.Is(err, wantErr) {
		t.Errorf("w.Write(report) error = %v, want %v", err, wantErr)
	}
}

// failingLineWriter fails to write program status lines, and accepts
// everything else.
type failingLineWriter struct{ err error }

func (f failingLineWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("# Program status:")) {
		return 0, f.err
	}
	return len(p), nil
}
