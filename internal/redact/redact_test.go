package redact

import (
	"testing"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/job/shell"
	"github.com/google/go-cmp/cmp"
)

func TestValuesToRedact(t *testing.T) {
	t.Parallel()

	redactConfig := []string{
		"*_PASSWORD",
		"*_TOKEN",
	}
	environment := []env.Pair{
		{Name: "BUILDKITE_PIPELINE", Value: "unit-test"},
		// These are example values, and are not leaked credentials
		{Name: "DATABASE_USERNAME", Value: "AzureDiamond"},
		{Name: "DATABASE_PASSWORD", Value: "hunter2"},
	}

	got := Values(shell.DiscardLogger, redactConfig, environment)
	want := []string{"hunter2"}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Values(%q, %q) diff (-got +want)\n%s", redactConfig, environment, diff)
	}
}

func TestValuesToRedactEmpty(t *testing.T) {
	t.Parallel()

	redactConfig := []string{}
	environment := []env.Pair{
		{Name: "FOO", Value: "BAR"},
		{Name: "BUILDKITE_PIPELINE", Value: "unit-test"},
	}

	got := Values(shell.DiscardLogger, redactConfig, environment)
	if len(got) != 0 {
		t.Errorf("Values(%q, %q) = %q, want empty slice", redactConfig, environment, got)
	}
}
