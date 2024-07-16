package redact

import (
	"testing"

	"github.com/buildkite/agent/v3/env"
	"github.com/google/go-cmp/cmp"
)

func TestVars(t *testing.T) {
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

	got, short, err := Vars(redactConfig, environment)
	if err != nil {
		t.Errorf("Vars(%q, %q) error = %v", redactConfig, environment, err)
	}
	if len(short) > 0 {
		t.Errorf("Vars(%q, %q) short = %q", redactConfig, environment, short)
	}
	want := []env.Pair{{Name: "DATABASE_PASSWORD", Value: "hunter2"}}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Vars(%q, %q) diff (-got +want)\n%s", redactConfig, environment, diff)
	}
}

func TestValuesToRedactEmpty(t *testing.T) {
	t.Parallel()

	redactConfig := []string{}
	environment := []env.Pair{
		{Name: "FOO", Value: "BAR"},
		{Name: "BUILDKITE_PIPELINE", Value: "unit-test"},
	}

	got, short, err := Vars(redactConfig, environment)
	if err != nil {
		t.Errorf("Vars(%q, %q) error = %v", redactConfig, environment, err)
	}
	if len(short) > 0 {
		t.Errorf("Vars(%q, %q) short = %q", redactConfig, environment, short)
	}
	if len(got) != 0 {
		t.Errorf("Vars(%q, %q) = %q, want empty slice", redactConfig, environment, got)
	}
}
