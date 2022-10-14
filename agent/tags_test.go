package agent

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
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

	assert.Contains(t, tags, "llamas")
	assert.Contains(t, tags, "rock")

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("os.Hostname() error = %v", err)
	}

	assert.Contains(t, tags, "hostname="+hostname)
	assert.Contains(t, tags, "os="+runtime.GOOS)
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

	assert.ElementsMatch(t, tags,
		[]string{"llamas", "rock", "aws:instance-id=i-blahblah", "aws:instance-type=t2.small", "custom_tag=true"})
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

	assert.ElementsMatch(t, tags,
		[]string{"custom_tag=true"})
}

func TestFetchingTagsFromECS(t *testing.T) {
	fetcher := &tagFetcher{
		ecsMetaDataDefault: func() (map[string]string, error) {
			return map[string]string{
				`ecs:container-name`: "ecs-buildkite-agent-blahblah",
				`ecs:image`:          "buildkite/agent",
				"ecs:task-arn":       "arn:aws:ecs:us-east-1:123456789012:task/MyCluster/4d590253bb114126b7afa7b58EXAMPLE",
			}, nil
		},
	}

	tags := fetcher.Fetch(context.Background(), logger.Discard, FetchTagsConfig{
		Tags:                []string{"llamas", "rock"},
		TagsFromECSMetaData: true,
	})

	assert.ElementsMatch(t, tags,
		[]string{
			"llamas",
			"rock",
			"ecs:container-name=ecs-buildkite-agent-blahblah",
			"ecs:image=buildkite/agent",
			"ecs:task-arn=arn:aws:ecs:us-east-1:123456789012:task/MyCluster/4d590253bb114126b7afa7b58EXAMPLE",
		})
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

	assert.ElementsMatch(t, tags,
		[]string{"llamas", "rock", "gcp:instance-id=my-instance", "gcp:zone=blah", "custom_tag=true"})
}

func TestFetchingTagsFromAllSources(t *testing.T) {
	fetcher := &tagFetcher{
		gcpMetaDataDefault: func() (map[string]string, error) {
			return map[string]string{`gcp_metadata`: "true"}, nil
		},
		gcpMetaDataPaths: func(paths map[string]string) (map[string]string, error) {
			assert.Equal(t, paths, map[string]string{"tag": "some/gcp/value"})
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
			assert.Equal(t, paths, map[string]string{"tag": "some/ec2/value"})
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

	assert.Contains(t, tags, "llamas")
	assert.Contains(t, tags, "rock")
	assert.Contains(t, tags, "gcp_metadata=true")
	assert.Contains(t, tags, "gcp_metadata_paths=true")
	assert.Contains(t, tags, "gcp_labels=true")
	assert.Contains(t, tags, "ec2_tags=true")
	assert.Contains(t, tags, "ec2_metadata=true")
	assert.Contains(t, tags, "ecs_metadata=true")
	assert.Contains(t, tags, "ec2_metadata_paths=true")
	assert.Contains(t, tags, "hostname="+hostname)
	assert.Contains(t, tags, "os="+runtime.GOOS)
}
