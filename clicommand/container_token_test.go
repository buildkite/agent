package clicommand

import (
	"fmt"
	"io"
	"os"
	"testing"
)

func TestConsumeContainerToken(t *testing.T) {
	for _, overridden := range []bool{false, true} {
		t.Run(fmt.Sprint(overridden), func(t *testing.T) {
			t.Cleanup(func() { containerTokenRef, containerTokenValue = "", "" })
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()
			if _, err := w.WriteString("environment-secret"); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			t.Setenv("BUILDKITE_AGENT_CONTAINER_TOKEN_FD", fmt.Sprint(r.Fd()))
			token, want := fmt.Sprintf("fd://%d", r.Fd()), "environment-secret"
			if overridden {
				token, want = "override-secret", "override-secret"
			}
			if err := ConsumeContainerToken(); err != nil {
				t.Fatal(err)
			}
			if got, err := resolveRegistrationToken(token); got != want || err != nil {
				t.Fatalf("token = %q, %v; want %q", got, err, want)
			}
			if _, ok := os.LookupEnv("BUILDKITE_AGENT_CONTAINER_TOKEN_FD"); ok {
				t.Fatal("handoff marker remains")
			}
			if _, err := io.ReadAll(r); err == nil {
				t.Fatal("handoff descriptor remains open")
			}
		})
	}
}
