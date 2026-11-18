package programstatus_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/buildkite/agent/v4/internal/programstatus"
	"github.com/buildkite/osc7501"
	"github.com/google/go-cmp/cmp"
)

func TestReports(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	r := programstatus.New(&out)
	var reports []osc7501.Report
	r.OnReport = func(report osc7501.Report) { reports = append(reports, report) }
	r.Working("build", 30, "Building")
	r.Finish("build", nil, "Built", "Build failed")
	r.Finish("build", errors.New("failed"), "Built", "Build failed")
	r.Finish("build", context.Canceled, "Built", "Build failed")
	r.Clear("build")
	r.Working("invalid id", -1, "Must not be emitted")
	want := []osc7501.Report{
		{ID: "build", App: "buildkite-agent", State: "working", Progress: 30, Msg: "Building"},
		{ID: "build", App: "buildkite-agent", State: "done", Progress: -1, Msg: "Built"},
		{ID: "build", App: "buildkite-agent", State: "error", Progress: -1, Msg: "Build failed"},
		{ID: "build", App: "buildkite-agent", State: "idle", Progress: -1},
		{ID: "build", App: "buildkite-agent", State: "clear", Progress: -1},
	}
	if diff := cmp.Diff(want, reports); diff != "" {
		t.Errorf("callback reports (-want +got):\n%s", diff)
	}
	var written []osc7501.Report
	scanner := osc7501.Scanner{W: io.Discard, OnReport: func(report osc7501.Report) { written = append(written, report) }}
	if _, err := scanner.Write(out.Bytes()); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, written); diff != "" {
		t.Errorf("written reports (-want +got):\n%s", diff)
	}
}

func TestEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		env        map[string]string
		isTerminal bool
		want       bool
	}{
		{name: "terminal", env: map[string]string{"TERM": "xterm"}, isTerminal: true, want: true},
		{name: "not a terminal", env: map[string]string{"TERM": "xterm"}, isTerminal: false, want: false},
		{name: "dumb terminal", env: map[string]string{"TERM": "dumb"}, isTerminal: true, want: false},
		{name: "in CI", env: map[string]string{"TERM": "xterm", "CI": "true"}, isTerminal: true, want: false},
		{name: "in a Buildkite job", env: map[string]string{"TERM": "xterm", "BUILDKITE": "true"}, isTerminal: true, want: false},
		{name: "turned on in a Buildkite job", env: map[string]string{"BUILDKITE": "true", "CI": "true", "BUILDKITE_PROGRAM_STATUS": "true"}, isTerminal: false, want: true},
		{name: "turned off in a terminal", env: map[string]string{"TERM": "xterm", "BUILDKITE_PROGRAM_STATUS": "0"}, isTerminal: true, want: false},
		{name: "not a boolean", env: map[string]string{"TERM": "xterm", "BUILDKITE_PROGRAM_STATUS": "auto"}, isTerminal: true, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lookupEnv := func(name string) (string, bool) {
				v, ok := test.env[name]
				return v, ok
			}
			if got := programstatus.Enabled(lookupEnv, test.isTerminal); got != test.want {
				t.Errorf("Enabled(%v, %t) = %t, want %t", test.env, test.isTerminal, got, test.want)
			}
		})
	}
}
