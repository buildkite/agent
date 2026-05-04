package agent

import (
	"context"
	"errors"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
)

func TestFetchingTags(t *testing.T) {
	cfg := FetchTagsConfig{
		Tags: []string{"llamas", "rock"},
	}
	tags, err := (&tagFetcher{}).Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("(&tagFetcher{}).Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}

	if diff := cmp.Diff(tags, []string{"llamas", "rock"}); diff != "" {
		t.Errorf("(*tagFetcher).Fetch() diff (-got +want):\n%v", diff)
	}
}

func TestFetchingTagsWithHostTags(t *testing.T) {
	cfg := FetchTagsConfig{
		Tags:         []string{"llamas", "rock"},
		TagsFromHost: true,
	}
	tags, err := (&tagFetcher{}).Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("(&tagFetcher{}).Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}

	if got, want := tags, "llamas"; !slices.Contains(got, want) {
		t.Errorf("(&tagFetcher{}).Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "rock"; !slices.Contains(got, want) {
		t.Errorf("(&tagFetcher{}).Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("os.Hostname() error = %v", err)
	}

	if got, want := tags, "hostname="+hostname; !slices.Contains(got, want) {
		t.Errorf("(&tagFetcher{}).Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "os="+runtime.GOOS; !slices.Contains(got, want) {
		t.Errorf("(&tagFetcher{}).Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
}

func TestFetchingTagsFromEC2(t *testing.T) {
	fetcher := &tagFetcher{
		ec2MetaDataDefault: func() (map[string]string, error) {
			return map[string]string{
				`aws:instance-id`:   "i-blahblah",
				`aws:instance-type`: "t2.small",
			}, nil
		},
		ec2Tags: func() (map[string]string, error) {
			return map[string]string{
				`custom_tag`: "true",
			}, nil
		},
	}

	cfg := FetchTagsConfig{
		Tags:                []string{"llamas", "rock"},
		TagsFromEC2MetaData: true,
		TagsFromEC2Tags:     true,
	}
	tags, err := fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}

	if diff := cmp.Diff(slices.Sorted(slices.Values(tags)), slices.Sorted(slices.Values([]string{"llamas", "rock", "aws:instance-id=i-blahblah", "aws:instance-type=t2.small", "custom_tag=true"}))); diff != "" {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) sorted diff (-got +want):\n%s", diff)
	}
}

func TestFetchingTagsFromEC2Tags(t *testing.T) {
	fetcher := &tagFetcher{
		ec2Tags: func() (map[string]string, error) {
			return map[string]string{
				`custom_tag`: "true",
			}, nil
		},
	}

	cfg := FetchTagsConfig{
		TagsFromEC2Tags: true,
	}
	tags, err := fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}

	if diff := cmp.Diff(slices.Sorted(slices.Values(tags)), slices.Sorted(slices.Values([]string{"custom_tag=true"}))); diff != "" {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) sorted diff (-got +want):\n%s", diff)
	}
}

func TestFetchingTagsFromECS(t *testing.T) {
	fetcher := &tagFetcher{
		ecsMetaDataDefault: func() (map[string]string, error) {
			return map[string]string{
				"ecs:container-name": "ecs-buildkite-agent-blahblah",
				"ecs:image":          "buildkite/agent",
				"ecs:task-arn":       "arn:aws:ecs:us-east-1:123456789012:task/MyCluster/4d590253bb114126b7afa7b58EXAMPLE",
			}, nil
		},
	}
	cfg := FetchTagsConfig{
		Tags:                []string{"llamas", "rock"},
		TagsFromECSMetaData: true,
	}
	tags, err := fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}

	if diff := cmp.Diff(slices.Sorted(slices.Values(tags)), slices.Sorted(slices.Values([]string{
		"llamas",
		"rock",
		"ecs:container-name=ecs-buildkite-agent-blahblah",
		"ecs:image=buildkite/agent",
		"ecs:task-arn=arn:aws:ecs:us-east-1:123456789012:task/MyCluster/4d590253bb114126b7afa7b58EXAMPLE",
	}))); diff != "" {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) sorted diff (-got +want):\n%s", diff)
	}
}

func TestFetchingTagsFromGCP(t *testing.T) {
	// Force test coverage of retry code, at the cost of 1000-2000ms.
	// This could be removed/improved later if we want faster tests.
	calls := 0
	fetcher := &tagFetcher{
		gcpMetaDataDefault: func() (map[string]string, error) {
			defer func() { calls++ }()
			if calls <= 0 {
				return nil, errors.New("transient failure, should retry")
			}
			return map[string]string{
				`gcp:instance-id`: "my-instance",
				`gcp:zone`:        "blah",
			}, nil
		},
		gcpLabels: func() (map[string]string, error) {
			return map[string]string{
				`custom_tag`: "true",
			}, nil
		},
	}

	cfg := FetchTagsConfig{
		Tags:                []string{"llamas", "rock"},
		TagsFromGCPMetaData: true,
		TagsFromGCPLabels:   true,
	}
	tags, err := fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}

	if diff := cmp.Diff(slices.Sorted(slices.Values(tags)), slices.Sorted(slices.Values([]string{"llamas", "rock", "gcp:instance-id=my-instance", "gcp:zone=blah", "custom_tag=true"}))); diff != "" {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) sorted diff (-got +want):\n%s", diff)
	}
}

func TestFetchingTagsFromAllSources(t *testing.T) {
	fetcher := &tagFetcher{
		gcpMetaDataDefault: func() (map[string]string, error) {
			return map[string]string{`gcp_metadata`: "true"}, nil
		},
		gcpMetaDataPaths: func(paths map[string]string) (map[string]string, error) {
			if diff := cmp.Diff(paths, map[string]string{"tag": "some/gcp/value"}); diff != "" {
				t.Errorf("paths diff (-got +want):\n%s", diff)
			}
			return map[string]string{`gcp_metadata_paths`: "true"}, nil
		},
		gcpLabels: func() (map[string]string, error) {
			return map[string]string{`gcp_labels`: "true"}, nil
		},
		ec2Tags: func() (map[string]string, error) {
			return map[string]string{`ec2_tags`: "true"}, nil
		},
		ec2MetaDataDefault: func() (map[string]string, error) {
			return map[string]string{`ec2_metadata`: "true"}, nil
		},
		ecsMetaDataDefault: func() (map[string]string, error) {
			return map[string]string{`ecs_metadata`: "true"}, nil
		},
		ec2MetaDataPaths: func(paths map[string]string) (map[string]string, error) {
			if diff := cmp.Diff(paths, map[string]string{"tag": "some/ec2/value"}); diff != "" {
				t.Errorf("paths diff (-got +want):\n%s", diff)
			}
			return map[string]string{`ec2_metadata_paths`: "true"}, nil
		},
	}

	cfg := FetchTagsConfig{
		Tags:                     []string{"llamas", "rock"},
		TagsFromGCPMetaData:      true,
		TagsFromGCPMetaDataPaths: []string{"tag=some/gcp/value"},
		TagsFromGCPLabels:        true,
		TagsFromHost:             true,
		TagsFromEC2MetaData:      true,
		TagsFromECSMetaData:      true,
		TagsFromEC2MetaDataPaths: []string{"tag=some/ec2/value"},
		TagsFromEC2Tags:          true,
	}
	tags, err := fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("os.Hostname() error = %v", err)
	}

	if got, want := tags, "llamas"; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "rock"; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "gcp_metadata=true"; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "gcp_metadata_paths=true"; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "gcp_labels=true"; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "ec2_tags=true"; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "ec2_metadata=true"; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "ecs_metadata=true"; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "ec2_metadata_paths=true"; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "hostname="+hostname; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "os="+runtime.GOOS; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
}

// Tests for --fail-on-missing-tags behavior

func TestFailOnMissingTags_EC2MetaData(t *testing.T) {
	fetcher := &tagFetcher{
		ec2MetaDataDefault: func() (map[string]string, error) {
			return nil, errors.New("IMDS unavailable")
		},
	}

	// With FailOnMissingTags=false (default), should soft-fail
	cfg := FetchTagsConfig{
		Tags:                []string{"llamas"},
		TagsFromEC2MetaData: true,
	}
	tags, err := fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}
	if diff := cmp.Diff(tags, []string{"llamas"}); diff != "" {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) diff (-got +want):\n%s", diff)
	}

	// With FailOnMissingTags=true, should return error
	cfg = FetchTagsConfig{
		Tags:                []string{"llamas"},
		TagsFromEC2MetaData: true,
		FailOnMissingTags:   true,
	}
	_, err = fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err == nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want non-nil error", err)
	}
	if got, want := err.Error(), "failed to fetch EC2 meta-data"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %q, want containing %q", got, want)
	}
}

func TestFailOnMissingTags_EC2MetaDataPaths(t *testing.T) {
	fetcher := &tagFetcher{
		ec2MetaDataPaths: func(paths map[string]string) (map[string]string, error) {
			return nil, errors.New("IMDS unavailable")
		},
	}

	// With FailOnMissingTags=false (default), should soft-fail
	cfg := FetchTagsConfig{
		Tags:                     []string{"llamas"},
		TagsFromEC2MetaDataPaths: []string{"tag=some/path"},
	}
	tags, err := fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}
	if diff := cmp.Diff(tags, []string{"llamas"}); diff != "" {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) diff (-got +want):\n%s", diff)
	}

	// With FailOnMissingTags=true, should return error
	cfg = FetchTagsConfig{
		Tags:                     []string{"llamas"},
		TagsFromEC2MetaDataPaths: []string{"tag=some/path"},
		FailOnMissingTags:        true,
	}
	_, err = fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err == nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want non-nil error", err)
	}
	if got, want := err.Error(), "failed to fetch EC2 meta-data from paths"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %q, want containing %q", got, want)
	}
}

func TestFailOnMissingTags_EC2Tags(t *testing.T) {
	fetcher := &tagFetcher{
		ec2Tags: func() (map[string]string, error) {
			return nil, errors.New("RequestLimitExceeded")
		},
	}

	// With FailOnMissingTags=false (default), should soft-fail
	cfg := FetchTagsConfig{
		Tags:            []string{"llamas"},
		TagsFromEC2Tags: true,
	}
	tags, err := fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}
	if diff := cmp.Diff(tags, []string{"llamas"}); diff != "" {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) diff (-got +want):\n%s", diff)
	}

	// With FailOnMissingTags=true, should return error
	cfg = FetchTagsConfig{
		Tags:              []string{"llamas"},
		TagsFromEC2Tags:   true,
		FailOnMissingTags: true,
	}
	_, err = fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err == nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want non-nil error", err)
	}
	if got, want := err.Error(), "failed to fetch EC2 tags"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %q, want containing %q", got, want)
	}
}

func TestFailOnMissingTags_ECSMetaData(t *testing.T) {
	fetcher := &tagFetcher{
		ecsMetaDataDefault: func() (map[string]string, error) {
			return nil, errors.New("ECS metadata unavailable")
		},
	}

	// With FailOnMissingTags=false (default), should soft-fail
	cfg := FetchTagsConfig{
		Tags:                []string{"llamas"},
		TagsFromECSMetaData: true,
	}
	tags, err := fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}
	if diff := cmp.Diff(tags, []string{"llamas"}); diff != "" {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) diff (-got +want):\n%s", diff)
	}

	// With FailOnMissingTags=true, should return error
	cfg = FetchTagsConfig{
		Tags:                []string{"llamas"},
		TagsFromECSMetaData: true,
		FailOnMissingTags:   true,
	}
	_, err = fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err == nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want non-nil error", err)
	}
	if got, want := err.Error(), "failed to fetch ECS meta-data"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %q, want containing %q", got, want)
	}
}

func TestFailOnMissingTags_GCPMetaData(t *testing.T) {
	fetcher := &tagFetcher{
		gcpMetaDataDefault: func() (map[string]string, error) {
			return nil, errors.New("GCP metadata unavailable")
		},
	}

	// With FailOnMissingTags=false (default), should soft-fail
	cfg := FetchTagsConfig{
		Tags:                []string{"llamas"},
		TagsFromGCPMetaData: true,
	}
	tags, err := fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}
	if diff := cmp.Diff(tags, []string{"llamas"}); diff != "" {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) diff (-got +want):\n%s", diff)
	}

	// With FailOnMissingTags=true, should return error
	cfg = FetchTagsConfig{
		Tags:                []string{"llamas"},
		TagsFromGCPMetaData: true,
		FailOnMissingTags:   true,
	}
	_, err = fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err == nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want non-nil error", err)
	}
	if got, want := err.Error(), "failed to fetch GCP meta-data"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %q, want containing %q", got, want)
	}
}

func TestFailOnMissingTags_GCPMetaDataPaths(t *testing.T) {
	fetcher := &tagFetcher{
		gcpMetaDataPaths: func(paths map[string]string) (map[string]string, error) {
			return nil, errors.New("GCP metadata unavailable")
		},
	}

	// With FailOnMissingTags=false (default), should soft-fail
	cfg := FetchTagsConfig{
		Tags:                     []string{"llamas"},
		TagsFromGCPMetaDataPaths: []string{"tag=some/path"},
	}
	tags, err := fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}
	if diff := cmp.Diff(tags, []string{"llamas"}); diff != "" {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) diff (-got +want):\n%s", diff)
	}

	// With FailOnMissingTags=true, should return error
	cfg = FetchTagsConfig{
		Tags:                     []string{"llamas"},
		TagsFromGCPMetaDataPaths: []string{"tag=some/path"},
		FailOnMissingTags:        true,
	}
	_, err = fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err == nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want non-nil error", err)
	}
	if got, want := err.Error(), "failed to fetch GCP meta-data from paths"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %q, want containing %q", got, want)
	}
}

func TestFailOnMissingTags_GCPLabels(t *testing.T) {
	fetcher := &tagFetcher{
		gcpLabels: func() (map[string]string, error) {
			return nil, errors.New("GCP labels unavailable")
		},
	}

	// With FailOnMissingTags=false (default), should soft-fail
	cfg := FetchTagsConfig{
		Tags:              []string{"llamas"},
		TagsFromGCPLabels: true,
	}
	tags, err := fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}
	if diff := cmp.Diff(tags, []string{"llamas"}); diff != "" {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) diff (-got +want):\n%s", diff)
	}

	// With FailOnMissingTags=true, should return error
	cfg = FetchTagsConfig{
		Tags:              []string{"llamas"},
		TagsFromGCPLabels: true,
		FailOnMissingTags: true,
	}
	_, err = fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err == nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want non-nil error", err)
	}
	if got, want := err.Error(), "failed to fetch GCP instance labels"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %q, want containing %q", got, want)
	}
}

// Verify that FailOnMissingTags has no effect when cloud sources succeed
func TestFailOnMissingTags_NoErrorWhenSourcesSucceed(t *testing.T) {
	fetcher := &tagFetcher{
		ec2MetaDataDefault: func() (map[string]string, error) {
			return map[string]string{"aws:instance-id": "i-1234"}, nil
		},
		ec2Tags: func() (map[string]string, error) {
			return map[string]string{"env": "prod"}, nil
		},
	}

	cfg := FetchTagsConfig{
		Tags:                []string{"llamas"},
		TagsFromEC2MetaData: true,
		TagsFromEC2Tags:     true,
		FailOnMissingTags:   true,
	}
	tags, err := fetcher.Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("fetcher.Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}
	if got, want := tags, "llamas"; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "aws:instance-id=i-1234"; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "env=prod"; !slices.Contains(got, want) {
		t.Errorf("fetcher.Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
}

// Verify that FailOnMissingTags does not affect non-cloud sources (host, k8s)
func TestFailOnMissingTags_DoesNotAffectHostTags(t *testing.T) {
	cfg := FetchTagsConfig{
		Tags:              []string{"llamas"},
		TagsFromHost:      true,
		FailOnMissingTags: true,
	}
	tags, err := (&tagFetcher{}).Fetch(context.Background(), logger.Discard, cfg)
	if err != nil {
		t.Fatalf("(&tagFetcher{}).Fetch(context.Background(), logger.Discard, cfg) error = %v, want nil", err)
	}
	if got, want := tags, "llamas"; !slices.Contains(got, want) {
		t.Errorf("(&tagFetcher{}).Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
	if got, want := tags, "os="+runtime.GOOS; !slices.Contains(got, want) {
		t.Errorf("(&tagFetcher{}).Fetch(context.Background(), logger.Discard, cfg) = %v, want containing %q", got, want)
	}
}
