package clicommand

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urfave/cli/v3"
)

func metaDataScopeTestCommand(command *cli.Command) *cli.Command {
	cmd := *command
	cmd.Flags = slices.Clone(cmd.Flags)
	for i, flag := range cmd.Flags {
		if flag, ok := flag.(*cli.StringFlag); ok {
			copy := *flag
			cmd.Flags[i] = &copy
		}
	}
	cmd.Writer = io.Discard
	return &cmd
}

func TestMetaDataScope(t *testing.T) {
	t.Setenv("BUILDKITE_JOB_ID", "current-job")
	t.Setenv("BUILDKITE_METADATA_BUILD_ID", "")
	t.Setenv("BUILDKITE_METADATA_SCOPE", "")
	if err := os.Unsetenv("BUILDKITE_METADATA_SCOPE"); err != nil {
		t.Fatal(err)
	}
	commands := []struct {
		command  *cli.Command
		args     []string
		response string
	}{
		{MetaDataSetCommand, []string{"key", "value"}, `{}`},
		{MetaDataSetBatchCommand, []string{"key=value"}, `{}`},
		{MetaDataGetCommand, []string{"key"}, `{"key":"key","value":"value"}`},
		{MetaDataExistsCommand, []string{"key"}, `{"exists":true}`},
		{MetaDataKeysCommand, nil, `["key"]`},
	}
	for _, command := range commands {
		for _, tc := range []struct {
			name    string
			flags   []string
			path    string
			scope   string
			wantErr string
		}{
			{"default", nil, "/jobs/current-job", "", ""},
			{"build", []string{"--scope", "build"}, "/jobs/current-job", "", ""},
			{"other job build", []string{"--job", "other-job"}, "/jobs/other-job", "", ""},
			{"job", []string{"--scope", "job"}, "/jobs/current-job", "job", ""},
			{"other job", []string{"--scope", "job", "--job", "other-job"}, "/jobs/other-job", "job", ""},
			{"invalid", []string{"--scope", "invalid"}, "", "", "scope must be build or job"},
		} {
			t.Run(command.command.Name+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.Method != "POST" || r.URL.Path != tc.path+"/data/"+command.command.Name {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					}
					var body map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
						t.Error(err)
					}
					var scope string
					if raw, ok := body["scope"]; ok {
						if tc.scope == "" {
							t.Error("build scope must be omitted for compatibility with older servers")
						}
						if err := json.Unmarshal(raw, &scope); err != nil {
							t.Error(err)
						}
					}
					if scope != tc.scope {
						t.Errorf("scope = %q, want %q", scope, tc.scope)
					}
					fmt.Fprint(w, command.response)
				}))
				defer server.Close()
				args := append([]string{command.command.Name}, command.args...)
				args = append(args, "--agent-access-token", "token", "--endpoint", server.URL)
				args = append(args, tc.flags...)
				cmd := metaDataScopeTestCommand(command.command)
				err := cmd.Run(t.Context(), args)
				if tc.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
						t.Fatalf("error = %v, want %q", err, tc.wantErr)
					}
					if calls.Load() != 0 {
						t.Fatal("invalid scope made an API request")
					}
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestMetaDataReadBuildSelector(t *testing.T) {
	t.Setenv("BUILDKITE_JOB_ID", "current-job")
	t.Setenv("BUILDKITE_METADATA_BUILD_ID", "other-build")
	t.Setenv("BUILDKITE_METADATA_SCOPE", "build")
	for _, command := range []struct {
		command  *cli.Command
		args     []string
		response string
	}{
		{MetaDataGetCommand, []string{"key"}, `{"value":"value"}`},
		{MetaDataExistsCommand, []string{"key"}, `{"exists":true}`},
		{MetaDataKeysCommand, nil, `[]`},
	} {
		for _, tc := range []struct {
			name    string
			flags   []string
			wantErr bool
		}{
			{"default env build", nil, false},
			{"explicit build", []string{"--build", "other-build"}, false},
			{"explicit scope build", []string{"--scope", "build"}, false},
			{"job rejects env build", []string{"--scope", "job"}, true},
			{"job rejects explicit build", []string{"--scope", "job", "--build", "other-build"}, true},
		} {
			t.Run(command.command.Name+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.URL.Path != "/builds/other-build/data/"+command.command.Name {
						t.Errorf("path = %s", r.URL.Path)
					}
					fmt.Fprint(w, command.response)
				}))
				defer server.Close()
				args := append([]string{command.command.Name}, command.args...)
				args = append(args, "--job", "other-job", "--endpoint", server.URL, "--agent-access-token", "token")
				args = append(args, tc.flags...)
				cmd := metaDataScopeTestCommand(command.command)
				err := cmd.Run(t.Context(), args)
				if tc.wantErr {
					if err == nil || !strings.Contains(err.Error(), "--scope job cannot be used with --build") {
						t.Fatalf("error = %v", err)
					}
					if calls.Load() != 0 {
						t.Fatal("invalid combination made an API request")
					}
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
