package clicommand

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/jobapi"
)

func TestEnvSetStdin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format string
		input  string
	}{
		{"multiline JSON", "json", "{\n  \"ALPACA\": \"Geronimo\"\n}\n"},
		{"one-line JSON", "json", "{\"ALPACA\":\"Geronimo\"}\n"},
		{"one JSON object per line", "json", "{\"ALPACA\":\"old\"}\n{\"ALPACA\":\"Geronimo\"}\n"},
		{"plain", "plain", "LLAMA=Kuzco\nALPACA=Geronimo\n"},
		{"malformed JSON", "json", "{\"ALPACA\":\"Geronimo\"}\n{"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdin, err := os.CreateTemp(t.TempDir(), "stdin")
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			if _, err := stdin.WriteString(tc.input); err != nil {
				t.Fatal(err)
			}
			if _, err := stdin.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			originalStdin := os.Stdin
			os.Stdin = stdin
			defer func() { os.Stdin = originalStdin }()

			socketPath, err := jobapi.NewSocketPath(os.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			jobEnv := env.New()
			redactors := replacer.NewMux(redact.New(io.Discard, nil))
			server, token, err := jobapi.NewServer(shell.TestingLogger{T: t}, socketPath, jobEnv, redactors)
			if err != nil {
				t.Fatal(err)
			}
			if err := server.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = server.Stop() })
			t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", socketPath)
			t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", token)

			cmd := *EnvSetCommand
			err = cmd.Run(t.Context(), []string{"set", "--input-format=" + tc.format, "--output-format=quiet", "-"})
			got, exists := jobEnv.Get("ALPACA")
			if tc.name == "malformed JSON" {
				if err == nil || !strings.Contains(err.Error(), "couldn't parse input") || exists {
					t.Fatalf("env set error = %v, ALPACA present = %t; want parse error and no update", err, exists)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !exists || got != "Geronimo" {
				t.Errorf("ALPACA = %q, present = %t; want Geronimo", got, exists)
			}
		})
	}
}
