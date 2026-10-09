package clicommand

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/jobapi"
	"github.com/urfave/cli/v3"
)

func TestParseGitCredentialInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		lines       []string
		expected    string
		expectedErr error
	}{
		{
			name: "happy path",
			lines: []string{
				"protocol=https",
				"host=github.com",
				"path=buildkite/agent",
			},
			expected: "https://github.com/buildkite/agent",
		},
		{
			name: "missing protocol",
			lines: []string{
				"host=github.com",
				"path=buildkite/agent",
			},
			expectedErr: errMissingComponent,
		},
		{
			name: "missing host",
			lines: []string{
				"protocol=https",
				"path=buildkite/agent",
			},
			expectedErr: errMissingComponent,
		},
		{
			name: "missing path",
			lines: []string{
				"protocol=https",
				"host=github.com",
			},
			expectedErr: errMissingComponent,
		},
		{
			name: "non-https protocol",
			lines: []string{
				"protocol=ssh",
				"host=github.com",
				"path=buildkite/agent",
			},
			expectedErr: errNotHTTPS,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			input := strings.Join(tc.lines, "\n")
			actual, actualErr := parseGitURLFromCredentialInput(input)
			if !errors.Is(actualErr, tc.expectedErr) {
				t.Fatalf("parseGitURLFromCredentialInput(%q) = error(%q), want error(%q)", input, actualErr, tc.expectedErr)
			}

			if actual != tc.expected {
				t.Fatalf("parseGitURLFromCredentialInput(%q) = %q, want %q", input, actual, tc.expected)
			}
		})
	}
}

func TestGitCredentialsHelperCommand(t *testing.T) {
	tests := []struct {
		name       string
		action     string
		input      string
		status     int
		token      string
		username   string
		wantRepo   string
		wantOutput string
		wantError  bool
		wantReason string
	}{
		{
			name:       "GitHub URL",
			action:     "get",
			input:      "protocol=https\nhost=github.com\npath=acme/widgets.git\n",
			status:     http.StatusOK,
			token:      "github-token",
			wantRepo:   "https://github.com/acme/widgets.git",
			wantOutput: "username=token\npassword=github-token\n\n",
		},
		{
			name:       "provider URL",
			action:     "get",
			input:      "protocol=https\nhost=git.example.com\npath=acme/widgets.git\n",
			status:     http.StatusOK,
			token:      "provider-token",
			wantRepo:   "https://git.example.com/acme/widgets.git",
			wantOutput: "username=token\npassword=provider-token\n\n",
		},
		{
			name:       "Code Storage username",
			action:     "get",
			input:      "protocol=https\nhost=acme.code.storage\npath=team/widgets.git\n",
			status:     http.StatusOK,
			token:      "pierre-jwt",
			username:   "t",
			wantRepo:   "https://acme.code.storage/team/widgets.git",
			wantOutput: "username=t\npassword=pierre-jwt\n\n",
		},
		{
			name:       "credential line injection",
			action:     "get",
			input:      "protocol=https\nhost=acme.code.storage\npath=team/widgets.git\n",
			status:     http.StatusOK,
			token:      "pierre-jwt",
			username:   "t\npassword=injected",
			wantRepo:   "https://acme.code.storage/team/widgets.git",
			wantOutput: "username=fail\npassword=fail\n\n",
			wantError:  true,
			wantReason: "Buildkite's response contained invalid characters.",
		},
		{
			name:       "malformed input",
			action:     "get",
			input:      "protocol=https\nhost=git.example.com\n",
			wantOutput: "username=fail\npassword=fail\n\n",
			wantError:  true,
			wantReason: "Git's credential request did not name an HTTPS repository",
		},
		{
			name:       "HTTP failure",
			action:     "get",
			input:      "protocol=https\nhost=git.example.com\npath=acme/widgets.git\n",
			status:     http.StatusBadRequest,
			wantRepo:   "https://git.example.com/acme/widgets.git",
			wantOutput: "username=fail\npassword=fail\n\n",
			wantError:  true,
			wantReason: "The request to Buildkite failed: POST ",
		},
		{
			name:       "empty token",
			action:     "get",
			input:      "protocol=https\nhost=git.example.com\npath=acme/widgets.git\n",
			status:     http.StatusOK,
			wantRepo:   "https://git.example.com/acme/widgets.git",
			wantOutput: "username=fail\npassword=fail\n\n",
			wantError:  true,
			wantReason: "Buildkite returned an empty token.",
		},
		{
			name:   "non-get action is a no-op",
			action: "store",
			input:  "protocol=https\nhost=git.example.com\npath=acme/widgets.git\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				requests++
				if got, want := req.URL.Path, "/jobs/job-id/repository_access_token"; got != want {
					t.Errorf("path = %q, want %q", got, want)
				}
				var requestBody string
				_, _ = fmt.Fscan(req.Body, &requestBody)
				if test.wantRepo != "" && !strings.Contains(requestBody, test.wantRepo) {
					t.Errorf("request body = %q, want repo %q", requestBody, test.wantRepo)
				}
				if test.status != 0 {
					rw.WriteHeader(test.status)
				}
				if test.status == http.StatusOK {
					_, _ = fmt.Fprintf(rw, `{"token":%q,"username":%q}`, test.token, test.username)
				}
			}))
			t.Cleanup(server.Close)

			reports := startAgentErrorTestServer(t)
			output, err := runGitCredentialsHelperCommand(t, server.URL, test.action, test.input)
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError %t", err, test.wantError)
			}
			if output != test.wantOutput {
				t.Errorf("output = %q, want %q", output, test.wantOutput)
			}
			wantRequests := 1
			if test.action != "get" || test.wantRepo == "" {
				wantRequests = 0
			}
			if requests != wantRequests {
				t.Errorf("requests = %d, want %d", requests, wantRequests)
			}
			if test.wantReason == "" {
				if len(*reports) != 0 {
					t.Errorf("reports = %+v, want none", *reports)
				}
				return
			}
			const lead = "The Git credential helper could not get repository credentials, so Git authentication will fail. "
			if len(*reports) != 1 || (*reports)[0].Code != "repository_credentials_failed" || !strings.HasPrefix((*reports)[0].Message, lead+test.wantReason) {
				t.Fatalf("reports = %+v, want repository_credentials_failed with reason %q", *reports, test.wantReason)
			}
			if message := (*reports)[0].Message; strings.Contains(message, "pierre-jwt") || strings.HasPrefix(test.wantReason, "The request") != strings.Contains(message, "400 Bad Request") {
				t.Errorf("message = %q, want only an HTTP failure's response and never a token", message)
			}
		})
	}
}

func TestGitCredentialsHelperRedaction(t *testing.T) {
	const secret = `repository-token\with-escaped-form`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"token":%q}`, secret)
	}))
	t.Cleanup(upstream.Close)
	for _, mode := range []string{"registered", "registration rejected", "socket missing"} {
		t.Run(mode, func(t *testing.T) {
			var reported *jobapi.CapturedError
			startCaptureErrorTestServer(t, func(_ context.Context, report *jobapi.CapturedError) error {
				reported = report
				return nil
			})
			// Registration must work independently of error-capture capability.
			t.Setenv("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", "")
			client, err := jobapi.NewDefaultClient(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "registration rejected":
				t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", "wrong-token")
			case "socket missing":
				t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
				t.Setenv("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", "true")
			}
			output, err := runGitCredentialsHelperCommand(t, upstream.URL, "get", "protocol=https\nhost=example.com\npath=repo.git\n")
			if mode != "registered" {
				if err == nil || output != "username=fail\npassword=fail\n\n" {
					t.Fatalf("output=%q, error=%v; want dummy credentials on registration failure", output, err)
				}
				return
			}
			if err != nil || output != "username=token\npassword="+secret+"\n\n" {
				t.Fatalf("output=%q, error=%v; want repository credentials", output, err)
			}
			if _, err := client.CaptureError(t.Context(), &jobapi.CapturedError{
				Code: "git_authentication_failed", Message: fmt.Sprintf("remote echoed %s and %q", secret, secret),
			}); err != nil {
				t.Fatal(err)
			}
			if reported == nil || reported.Message != `remote echoed [REDACTED] and "[REDACTED]"` {
				t.Fatalf("reported=%+v, want raw and escaped token redacted", reported)
			}
		})
	}
}

func runGitCredentialsHelperCommand(t *testing.T, endpoint, action, input string) (string, error) {
	t.Helper()

	readStdin, writeStdin, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeStdin.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err := writeStdin.Close(); err != nil {
		t.Fatal(err)
	}
	originalStdin := os.Stdin
	os.Stdin = readStdin
	t.Cleanup(func() {
		os.Stdin = originalStdin
		_ = readStdin.Close()
	})

	// Clone the command, because Run mutates it, which breaks the command completeness test.
	cmd := *GitCredentialsHelperCommand
	var output strings.Builder
	app := &cli.Command{
		Name:           "buildkite-agent",
		Commands:       []*cli.Command{&cmd},
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Writer:         &output,
	}
	err = app.Run(t.Context(), []string{
		"buildkite-agent",
		"git-credentials-helper",
		"--job-id", "job-id",
		"--endpoint", endpoint,
		"--agent-access-token", "agent-token",
		action,
	})
	return output.String(), err
}
