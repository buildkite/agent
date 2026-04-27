package agent

import (
	"context"
	"errors"
	"os"
	"runtime"
	"slices"
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-cmp/cmp"
)

func TestFetchingTags(t *testing.T) {
	tags := (&tagFetcher{}).Fetch(context.Background(), logger.Discard, FetchTagsConfig{
		Tags: []string{"llamas", "rock"},
	})

	if diff := cmp.Diff(tags, []string{"llamas", "rock"}); diff != "" {
		t.Errorf("(*tagFetcher).Fetch() diff (-got +want):\n%v", diff)
	}
}

func TestFetchingTagsWithHostTags(t *testing.T) {
	tags := (&tagFetcher{}).Fetch(context.Background(), logger.Discard, FetchTagsConfig{
		Tags:         []string{"llamas", "rock"},
		TagsFromHost: true,
	})

	if got, want := tags, "llamas"; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}
	if got, want := tags, "rock"; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("os.Hostname() error = %v", err)
	}

	if got, want := tags, "hostname="+hostname; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}
	if got, want := tags, "os="+runtime.GOOS; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
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

	tags := fetcher.Fetch(context.Background(), logger.Discard, FetchTagsConfig{
		Tags:                []string{"llamas", "rock"},
		TagsFromEC2MetaData: true,
		TagsFromEC2Tags:     true,
	})

	slices.Sort(tags)
	if diff := cmp.Diff(tags, slices.Sorted(slices.Values([]string{"llamas", "rock", "aws:instance-id=i-blahblah", "aws:instance-type=t2.small", "custom_tag=true"}))); diff != "" {
		t.Errorf("tags diff (-got +want):\n%s", diff)
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

	tags := fetcher.Fetch(context.Background(), logger.Discard, FetchTagsConfig{
		TagsFromEC2Tags: true,
	})

	slices.Sort(tags)
	if diff := cmp.Diff(tags, slices.Sorted(slices.Values([]string{"custom_tag=true"}))); diff != "" {
		t.Errorf("tags diff (-got +want):\n%s", diff)
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

	tags := fetcher.Fetch(context.Background(), logger.Discard, FetchTagsConfig{
		Tags:                []string{"llamas", "rock"},
		TagsFromECSMetaData: true,
	})

	slices.Sort(tags)
	if diff := cmp.Diff(tags, slices.Sorted(slices.Values([]string{
		"llamas",
		"rock",
		"ecs:container-name=ecs-buildkite-agent-blahblah",
		"ecs:image=buildkite/agent",
		"ecs:task-arn=arn:aws:ecs:us-east-1:123456789012:task/MyCluster/4d590253bb114126b7afa7b58EXAMPLE",
	}))); diff != "" {
		t.Errorf("tags diff (-got +want):\n%s", diff)
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

	tags := fetcher.Fetch(context.Background(), logger.Discard, FetchTagsConfig{
		Tags:                []string{"llamas", "rock"},
		TagsFromGCPMetaData: true,
		TagsFromGCPLabels:   true,
	})

	slices.Sort(tags)
	if diff := cmp.Diff(tags, slices.Sorted(slices.Values([]string{"llamas", "rock", "gcp:instance-id=my-instance", "gcp:zone=blah", "custom_tag=true"}))); diff != "" {
		t.Errorf("tags diff (-got +want):\n%s", diff)
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

	tags := fetcher.Fetch(context.Background(), logger.Discard, FetchTagsConfig{
		Tags:                     []string{"llamas", "rock"},
		TagsFromGCPMetaData:      true,
		TagsFromGCPMetaDataPaths: []string{"tag=some/gcp/value"},
		TagsFromGCPLabels:        true,
		TagsFromHost:             true,
		TagsFromEC2MetaData:      true,
		TagsFromECSMetaData:      true,
		TagsFromEC2MetaDataPaths: []string{"tag=some/ec2/value"},
		TagsFromEC2Tags:          true,
	})

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("os.Hostname() error = %v", err)
	}

	if got, want := tags, "llamas"; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}
	if got, want := tags, "rock"; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}
	if got, want := tags, "gcp_metadata=true"; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}
	if got, want := tags, "gcp_metadata_paths=true"; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}
	if got, want := tags, "gcp_labels=true"; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}
	if got, want := tags, "ec2_tags=true"; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}
	if got, want := tags, "ec2_metadata=true"; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}
	if got, want := tags, "ecs_metadata=true"; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}
	if got, want := tags, "ec2_metadata_paths=true"; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}
	if got, want := tags, "hostname="+hostname; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}
	if got, want := tags, "os="+runtime.GOOS; !slices.Contains(got, want) {
		t.Errorf("tags = %v, want containing %q", got, want)
	}
}
