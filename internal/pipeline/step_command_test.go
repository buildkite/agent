package pipeline

import (
	"testing"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/google/go-cmp/cmp"
)

func TestCommandStepUnmarshalJSON(t *testing.T) {
	// AcceptJob returns a Step that looks like this (but without the
	// indentation):
	input := []byte(`{
  "command": "script/buildkite/xxx.sh",
  "plugins": [
    {
      "github.com/xxx/aws-assume-role-buildkite-plugin#v0.1.0": {
        "role": "arn:aws:iam::xxx:role/xxx"
      }
    },
    {
      "github.com/buildkite-plugins/ecr-buildkite-plugin#v1.1.4": {
        "login": true,
        "account_ids": "xxx",
        "registry_region": "us-east-1"
      }
    },
    {
      "github.com/buildkite-plugins/docker-compose-buildkite-plugin#v2.5.1": {
        "run": "xxx",
        "config": ".buildkite/docker/docker-compose.yml",
        "env": [
          "AWS_ACCESS_KEY_ID",
          "AWS_SECRET_ACCESS_KEY",
          "AWS_SESSION_TOKEN"
        ]
      }
    }
  ]
}`)

	want := &CommandStep{
		Command: "script/buildkite/xxx.sh",
		Plugins: Plugins{
			{
				Source: "github.com/xxx/aws-assume-role-buildkite-plugin#v0.1.0",
				Config: ordered.MapFromItems(
					ordered.TupleSA{Key: "role", Value: "arn:aws:iam::xxx:role/xxx"},
				),
			},
			{
				Source: "github.com/buildkite-plugins/ecr-buildkite-plugin#v1.1.4",
				Config: ordered.MapFromItems(
					ordered.TupleSA{Key: "login", Value: true},
					ordered.TupleSA{Key: "account_ids", Value: "xxx"},
					ordered.TupleSA{Key: "registry_region", Value: "us-east-1"},
				),
			},
			{
				Source: "github.com/buildkite-plugins/docker-compose-buildkite-plugin#v2.5.1",
				Config: ordered.MapFromItems(
					ordered.TupleSA{Key: "run", Value: "xxx"},
					ordered.TupleSA{Key: "config", Value: ".buildkite/docker/docker-compose.yml"},
					ordered.TupleSA{Key: "env", Value: []any{
						"AWS_ACCESS_KEY_ID",
						"AWS_SECRET_ACCESS_KEY",
						"AWS_SESSION_TOKEN",
					}},
				),
			},
		},
	}

	got := new(CommandStep)
	if err := got.UnmarshalJSON(input); err != nil {
		t.Fatalf("CommandStep.UnmarshalJSON(input) = %v", err)
	}

	if diff := cmp.Diff(got, want, cmp.Comparer(ordered.EqualSA)); diff != "" {
		t.Errorf("CommandStep diff after UnmarshalJSON (-got +want):\n%s", diff)
	}
}
