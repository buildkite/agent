package process_test

import (
	"bytes"
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

	want := "progress: 40%" + compiling40 +
		"\n\x1b[90m# Program status: state=working id=build progress=40 app=cargo msg=\"Compiling\"\x1b[0m\n" +
		"\r\n" + compiling60 +
		"\x1b[32m" + linking +
		"\x1b[90m# Program status: state=working id=build app=cargo msg=\"Linking\"\x1b[0m\n" +
		"\x1b[0m" + noState + testsDone +
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
	want := clearAll + clearLine + building + buildingLine + clearAll + building + buildingLine
	if diff := cmp.Diff(want, out.String()); diff != "" {
		t.Errorf("output diff (-want +got):\n%s", diff)
	}
}
