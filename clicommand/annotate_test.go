package clicommand

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
)

func newAnnotateTestServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.RequestURI() {
		case "/jobs/jobid/annotations":
			_, _ = io.WriteString(rw, `{"context":"", style:"", body:"abc"}`)
		default:
			t.Errorf("unexpected HTTP request: %s %v", req.Method, req.URL.RequestURI())
		}
	}))
}

func TestAnnotate(t *testing.T) {
	server := newAnnotateTestServer(t)
	defer server.Close()

	cfg := AnnotateConfig{
		Body: "abc",
		Job:  "jobid",
		APIConfig: APIConfig{
			AgentAccessToken: "agentaccesstoken",
			Endpoint:         server.URL,
		},
		Priority: 1,
	}
	l := logger.NewBuffer()

	err := annotate(t.Context(), cfg, l)
	if err != nil {
		t.Errorf("annotate(ctx, %v, l) error = %v", cfg, err)
	}
	if want := "[debug] Successfully annotated build"; !slices.Contains(l.Messages, want) {
		t.Errorf("annotate(ctx, %v, l) logged messages = %q, missing %q", cfg, l.Messages, want)
	}
}

func TestAnnotateMaxBodySize(t *testing.T) {
	cfg := AnnotateConfig{
		Body: strings.Repeat("a", 1048577),
	}
	l := logger.NewBuffer()

	err := annotate(t.Context(), cfg, l)
	wantErr := annotationTooBigError{bodySize: 1048577}
	if !errors.Is(err, wantErr) {
		t.Errorf("annotate(ctx, AnnotateConfig{Body: \"aaa...aaa\"}, l) error = %v, want %[2]T %[2]v", err, wantErr)
	}
}

// Regression test for https://linear.app/buildkite/issue/A-1899. urfave/cli
// v3.11.0 trimmed leading/trailing whitespace from positional arguments
// (fixed upstream in urfave/cli#2423, released in v3.12.0), so
// `annotate --append $'\n* item one\n'` posted a body of "* item one" and
// appended Markdown list items merged into a single list. The body must
// reach the Agent API byte for byte.
//
// The bodies here use '*' bullets (the customer's exact scenario): a body
// whose trimmed copy starts with '-' is classified as "not a flag, stop
// parsing" and was never trimmed, so '- item' bodies do not exercise the
// regression.
func TestAnnotateCommandPreservesBodyWhitespace(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
		// If set, the body is passed after a "--" separator.
		afterDoubleDash bool
	}{
		{
			name: "body as positional argument",
			body: "\n* item one\n",
			want: "\n* item one\n",
		},
		{
			name: "leading and trailing whitespace",
			body: " \n\t* a\n* b \n\n",
			want: " \n\t* a\n* b \n\n",
		},
		{
			name:            "body after -- separator",
			body:            "\n* item one\n",
			want:            "\n* item one\n",
			afterDoubleDash: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var posted api.Annotation
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodPost || req.URL.RequestURI() != "/jobs/jobid/annotations" {
					t.Errorf("unexpected HTTP request: %s %v", req.Method, req.URL.RequestURI())
				}
				var annotation api.Annotation
				if err := json.NewDecoder(req.Body).Decode(&annotation); err != nil {
					t.Errorf("decoding posted annotation: %v", err)
				}
				mu.Lock()
				posted = annotation
				mu.Unlock()
				_, _ = rw.Write([]byte(`{}`))
			}))
			defer server.Close()

			// Connection flags must precede the "--" separator: everything
			// after "--" is treated as positional.
			args := []string{
				"annotate",
				"--agent-access-token", "agentaccesstoken",
				"--endpoint", server.URL,
				"--job", "jobid",
				"--append",
				"--context", "c",
			}
			if tt.afterDoubleDash {
				args = append(args, "--")
			}
			args = append(args, tt.body)

			// Run a copy of the command: urfave/cli mutates the command on
			// Run (adding a generated help subcommand/flag), which would
			// pollute the shared AnnotateCommand that the command registry
			// and structural tests use.
			cmd := *AnnotateCommand
			if err := cmd.Run(t.Context(), args); err != nil {
				t.Fatalf("AnnotateCommand.Run(%q) error = %v", args, err)
			}

			mu.Lock()
			defer mu.Unlock()
			if posted.Body != tt.want {
				t.Errorf("posted annotation body = %q, want %q", posted.Body, tt.want)
			}
			if !posted.Append {
				t.Errorf("posted annotation append = false, want true")
			}
		})
	}
}
