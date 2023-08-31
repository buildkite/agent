package pipeline

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestPluginsUnmarshalJSON(t *testing.T) {
	t.Parallel()

	src := []byte(`[
	{
		"xxx/aws-assume-role#v0.1.0": {
			"role": "arn:aws:iam::xxx:role/xxx"
		}
	},
	{
		"ecr#v1.1.4": {
			"account_ids": "xxx",
			"login": true,
			"registry_region": "us-east-1"
		}
	},
	{
		"docker-compose#v2.5.1": {
			"config": ".buildkite/docker/docker-compose.yml",
			"env": [
				"AWS_ACCESS_KEY_ID",
				"AWS_SECRET_ACCESS_KEY",
				"AWS_SESSION_TOKEN"
			],
			"run": "xxx"
		}
	}
]`)

	var got Plugins
	if err := json.Unmarshal(src, &got); err != nil {
		t.Fatalf("json.Unmarshal(%q, &got) = %v", src, err)
	}
	want := Plugins{
		{
			Source: "xxx/aws-assume-role#v0.1.0",
			Config: map[string]any{"role": "arn:aws:iam::xxx:role/xxx"},
		},
		{
			Source: "ecr#v1.1.4",
			Config: map[string]any{
				"login":           true,
				"account_ids":     "xxx",
				"registry_region": "us-east-1",
			},
		},
		{
			Source: "docker-compose#v2.5.1",
			Config: map[string]any{
				"run":    "xxx",
				"config": ".buildkite/docker/docker-compose.yml",
				"env": []any{
					"AWS_ACCESS_KEY_ID",
					"AWS_SECRET_ACCESS_KEY",
					"AWS_SESSION_TOKEN",
				},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Unmarshaled Plugin diff (-got +want):\n%s", diff)
	}

}
