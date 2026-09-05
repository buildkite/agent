package dockerbootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestSetupFailureIncludesDockerDiagnostic(t *testing.T) {
	for _, phase := range []string{"info", "pull", "image identity", "create", "rm"} {
		t.Run(phase, func(t *testing.T) {
			const detail = "permission denied while trying to connect to unix:///var/run/docker.sock"
			f := &fakeClient{run: func(_ context.Context, args []string, _ map[string]string, stdout, stderr io.Writer) (int, error) {
				if phase == "pull" && args[0] == "image" {
					return 1, nil
				}
				fail := args[0] == phase || (phase == "image identity" && args[0] == "image" && len(args) > 3)
				if fail {
					_, _ = fmt.Fprintln(stdout, "sensitive inspection stdout")
					_, _ = fmt.Fprintln(stderr, detail)
					return 1, nil
				}
				if phase == "rm" && args[0] == "ps" {
					_, _ = fmt.Fprintln(stdout, "still-exists")
				}
				return 0, nil
			}}
			code, err := (Runner{Client: f}).Run(t.Context(), testConfig(t))
			if code != SetupFailure || err == nil {
				t.Fatalf("got (%d,%v)", code, err)
			}
			if !strings.Contains(err.Error(), detail) || !strings.Contains(err.Error(), "exit 1") {
				t.Fatalf("missing diagnostic: %v", err)
			}
			if strings.Contains(err.Error(), "sensitive inspection stdout") {
				t.Fatal("included inspection stdout")
			}
		})
	}
}

func TestDiagnosticRedaction(t *testing.T) {
	f := &fakeClient{run: func(_ context.Context, _ []string, _ map[string]string, _, stderr io.Writer) (int, error) {
		// Write across chunks as os/exec can do; include a quoted multiline value.
		for _, part := range []string{"private-", "token custom-value short-key https://user:unknown-password@registry.test/v2/ Authorization: Bearer unknown-token ", `"line-one\nline-two"`} {
			_, _ = io.WriteString(stderr, part)
		}
		return 1, nil
	}}
	c := withDiagnostics(f, []string{
		"BUILDKITE_AGENT_ACCESS_TOKEN=private-token", "BUILDKITE_REDACTED_VARS=CUSTOM", "CUSTOM=custom-value",
		"SSH_KEY=short-key", "MULTILINE_SECRET=line-one\nline-two",
	})
	_, err := c.Run(t.Context(), []string{"pull", "image"}, nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, secret := range []string{"private-token", "custom-value", "short-key", "unknown-password", "unknown-token", "line-one", "line-two"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("leaked %q: %v", secret, err)
		}
	}
	if !strings.Contains(err.Error(), "registry.test/v2/") {
		t.Fatal("lost registry diagnostic context")
	}
}

func TestDiagnosticLimitRedactsBeforeTruncating(t *testing.T) {
	var capture diagnosticBuffer
	f := &fakeClient{run: func(_ context.Context, _ []string, _ map[string]string, _, stderr io.Writer) (int, error) {
		_, _ = io.WriteString(stderr, "useful first line\n"+strings.Repeat("x", diagnosticLimit-30)+"secret-prefix\nsecret-suffix\n"+strings.Repeat("y", diagnosticLimit))
		return 1, nil
	}}
	c := withDiagnostics(f, []string{"A_TOKEN=secret-prefix\nsecret-suffix"})
	_, err := c.Run(t.Context(), []string{"info"}, nil, io.Discard, &capture)
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("missing truncation marker: %v", err)
	}
	if len(err.Error()) > diagnosticLimit+100 || strings.Contains(err.Error(), "secret-prefix") || strings.Contains(err.Error(), "secret-suffix") {
		t.Fatal("oversized or unredacted diagnostic")
	}
	if !strings.Contains(err.Error(), "useful first line") {
		t.Fatal("lost complete diagnostic line")
	}
	_, _ = capture.Write(bytes.Repeat([]byte("x"), diagnosticLimit*2))
	if len(capture.data) != diagnosticLimit {
		t.Fatal("capture was not bounded")
	}
}

func TestDiagnosticPreservesContextError(t *testing.T) {
	f := &fakeClient{run: func(_ context.Context, _ []string, _ map[string]string, _, stderr io.Writer) (int, error) {
		_, _ = io.WriteString(stderr, "waiting for daemon")
		return 0, context.DeadlineExceeded
	}}
	_, err := withDiagnostics(f, nil).Run(t.Context(), []string{"info"}, nil, io.Discard, io.Discard)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "waiting for daemon") {
		t.Fatalf("got %v", err)
	}
}
