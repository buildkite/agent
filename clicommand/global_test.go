package clicommand

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/urfave/cli/v3"
)

func TestParseLogLevel(t *testing.T) {
	tests := map[string]slog.Level{
		"":        slog.LevelInfo,
		"DEBUG":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"fatal":   slog.LevelError,
	}

	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := parseLogLevel(input)
			if err != nil {
				t.Fatalf("parseLogLevel(%q) error = %v", input, err)
			}
			if got != want {
				t.Errorf("parseLogLevel(%q) = %v, want %v", input, got, want)
			}
		})
	}

	if _, err := parseLogLevel("notice"); err == nil {
		t.Error("parseLogLevel(\"notice\") error = nil, want non-nil")
	}
}

func TestCreateLoggerText(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previousStderr := os.Stderr
	os.Stderr = write
	t.Cleanup(func() {
		os.Stderr = previousStderr
		_ = read.Close()
		_ = write.Close()
	})

	l := CreateLogger(&struct {
		LogLevel  string
		LogFormat string
		NoColor   bool
	}{NoColor: true})
	os.Stderr = previousStderr
	l.Debug("hidden")
	l.Info("visible")
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}

	if got := string(output); !strings.Contains(got, "visible") || strings.Contains(got, "hidden") {
		t.Errorf("text log output = %q, want visible info but no debug message", got)
	}
	if strings.Contains(string(output), "\x1b[") {
		t.Errorf("text log output = %q, want no colour escape codes", output)
	}
	if matched := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2}) `).Match(output); !matched {
		t.Errorf("text log output = %q, want date, time, and UTC offset", output)
	}
}

func TestCreateLoggerJSON(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previousStdout := os.Stdout
	os.Stdout = write
	t.Cleanup(func() {
		os.Stdout = previousStdout
		_ = read.Close()
		_ = write.Close()
	})

	l := CreateLogger(&struct {
		LogLevel  string
		LogFormat string
	}{LogLevel: "debug", LogFormat: "json"})
	os.Stdout = previousStdout
	l.Debug("visible", slog.Int("count", 2))
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}

	var record map[string]any
	if err := json.Unmarshal(output, &record); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", output, err)
	}
	if got, want := record["level"], "DEBUG"; got != want {
		t.Errorf("level = %v, want %v", got, want)
	}
	if got, want := record["msg"], "visible"; got != want {
		t.Errorf("msg = %v, want %v", got, want)
	}
	if got, want := record["count"], float64(2); got != want {
		t.Errorf("count = %v (%T), want native JSON number %v", got, got, want)
	}
}

func TestAllFlagEnvs(t *testing.T) {
	// This is testing allFlagEnvs, not EnvDumpCommand, but it will certainly
	// detect any changes to env dump's flag env vars!
	got := slices.Sorted(allFlagEnvs(EnvDumpCommand))
	want := []string{
		"BUILDKITE_AGENT_DEBUG",
		"BUILDKITE_AGENT_ENV_DUMP_FORMAT",
		"BUILDKITE_AGENT_EXPERIMENT",
		"BUILDKITE_AGENT_LOG_LEVEL",
		"BUILDKITE_AGENT_NO_COLOR",
		"BUILDKITE_AGENT_PROFILE",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("allFlagEnvs(EnvDumpCommand) diff (-got +want):\n%s", diff)
	}
}

func TestGitCommitVerificationFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "defaults to strict",
			want: "strict",
		},
		{
			name: "can be overridden with off",
			args: []string{"--git-commit-verification", "off"},
			want: "off",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			flag := &cli.StringFlag{
				Name:             GitCommitVerificationFlag.Name,
				Value:            GitCommitVerificationFlag.Value,
				ValidateDefaults: GitCommitVerificationFlag.ValidateDefaults,
				Validator:        GitCommitVerificationFlag.Validator,
			}

			var got string
			command := &cli.Command{
				Name:  "test",
				Flags: []cli.Flag{flag},
				Action: func(_ context.Context, command *cli.Command) error {
					got = command.String("git-commit-verification")
					return nil
				},
			}

			if err := command.Run(t.Context(), append([]string{"test"}, test.args...)); err != nil {
				t.Fatalf("command.Run() error = %v", err)
			}
			if got != test.want {
				t.Errorf("git-commit-verification = %q, want %q", got, test.want)
			}
		})
	}
}
