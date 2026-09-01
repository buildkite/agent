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

type testConfig struct {
	Debug     bool
	LogLevel  string
	LogFormat string
	NoColor   bool
}

func TestLogLevelFromConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  testConfig
		want slog.Level
	}{
		{name: "info", cfg: testConfig{LogLevel: "info"}, want: slog.LevelInfo},
		{name: "case insensitive", cfg: testConfig{LogLevel: "ERROR"}, want: slog.LevelError},
		{name: "level offset", cfg: testConfig{LogLevel: "INFO+2"}, want: slog.LevelInfo + 2},
		{name: "debug overrides log level", cfg: testConfig{Debug: true, LogLevel: "error"}, want: slog.LevelDebug},
		{name: "debug bypasses invalid log level", cfg: testConfig{Debug: true, LogLevel: "invalid"}, want: slog.LevelDebug},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := logLevelFromConfig(&test.cfg)
			if err != nil {
				t.Fatalf("logLevelFromConfig(%+v) error = %v", test.cfg, err)
			}
			if got != test.want {
				t.Errorf("logLevelFromConfig(%+v) = %v, want %v", test.cfg, got, test.want)
			}
		})
	}
}

func TestLogLevelFromConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     any
		wantErr string
	}{
		{name: "invalid level", cfg: &struct {
			LogLevel string
		}{LogLevel: "invalid"}, wantErr: "parsing log level"},
		{name: "non-string level", cfg: &struct {
			LogLevel int
		}{LogLevel: 1}, wantErr: "couldn't convert LogLevel field into string"},
		{name: "missing level", cfg: &struct{}{}, wantErr: "getting log level from config struct"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := logLevelFromConfig(test.cfg)
			if err == nil {
				t.Fatalf("logLevelFromConfig(%+v) error = nil, want error containing %q", test.cfg, test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("logLevelFromConfig(%+v) error = %q, want error containing %q", test.cfg, err, test.wantErr)
			}
		})
	}
}

func TestNoColorRequested(t *testing.T) {
	tests := []struct {
		name    string
		flag    bool
		environ string
		want    bool
	}{
		{name: "not requested"},
		{name: "agent flag", flag: true, want: true},
		{name: "NO_COLOR arbitrary value", environ: "yes", want: true},
		{name: "NO_COLOR false is still present", environ: "false", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", test.environ)
			cfg := &testConfig{NoColor: test.flag}
			if got := noColorRequested(cfg); got != test.want {
				t.Errorf("noColorRequested(%+v) = %t, want %t", cfg, got, test.want)
			}
		})
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

	l := CreateLogger(&testConfig{LogLevel: "info", NoColor: true})
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

	l := CreateLogger(&testConfig{LogLevel: "debug", LogFormat: "json"})
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

func TestUnsetConfigFromEnvironmentPreservesNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "yes")
	if err := UnsetConfigFromEnvironment(EnvDumpCommand); err != nil {
		t.Fatalf("UnsetConfigFromEnvironment(EnvDumpCommand) error = %v", err)
	}
	if got, want := os.Getenv("NO_COLOR"), "yes"; got != want {
		t.Errorf("NO_COLOR = %q, want %q", got, want)
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
