package programstatus_test

import (
	"testing"

	"github.com/buildkite/agent/v4/internal/programstatus"
)

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
