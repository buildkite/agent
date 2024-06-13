package redact

import (
	"testing"

	"github.com/buildkite/agent/v3/internal/job/shell"
	"github.com/google/go-cmp/cmp"
)

func TestValuesToRedact(t *testing.T) {
	t.Parallel()

	redactConfig := []string{
		"*_PASSWORD",
		"*_TOKEN",
	}
	environment := map[string]string{
		"BUILDKITE_PIPELINE": "unit-test",
		// These are example values, and are not leaked credentials
		"DATABASE_USERNAME": "AzureDiamond",
		"DATABASE_PASSWORD": "hunter2",
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
	environment := map[string]string{
		"FOO":                "BAR",
		"BUILDKITE_PIPELINE": "unit-test",
	}

	got := Values(shell.DiscardLogger, redactConfig, environment)
	if len(got) != 0 {
		t.Errorf("Values(%q, %q) = %q, want empty slice", redactConfig, environment, got)
	}
}
