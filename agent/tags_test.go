package agent

import (
	"os"
	"reflect"
	"runtime"
	"testing"

	"github.com/buildkite/agent/logger"
	"github.com/stretchr/testify/assert"
)

func TestFetchingTags(t *testing.T) {
	tags := (&tagFetcher{}).Fetch(logger.Discard, FetchTagsConfig{
		Tags: []string{"llamas", "rock"},
	})

	if !reflect.DeepEqual(tags, []string{"llamas", "rock"}) {
		t.Fatalf("bad tags: %#v", tags)
	}
}

func TestFetchingTagsWithHostTags(t *testing.T) {
	tags := (&tagFetcher{}).Fetch(logger.Discard, FetchTagsConfig{
		Tags:         []string{"llamas", "rock"},
		TagsFromHost: true,
	})

	assert.Contains(t, tags, "llamas")
	assert.Contains(t, tags, "rock")

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}

	assert.Contains(t, tags, "hostname="+hostname)
	assert.Contains(t, tags, "os="+runtime.GOOS)
}

func TestFetchingTagsFromEC2(t *testing.T) {
	fetcher := &tagFetcher{
		ec2Metadata: func() (map[string]string, error) {
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

	tags := fetcher.Fetch(logger.Discard, FetchTagsConfig{
		Tags:            []string{"llamas", "rock"},
		TagsFromEC2:     true,
		TagsFromEC2Tags: true,
	})

	assert.ElementsMatch(t, tags,
		[]string{"llamas", "rock", "aws:instance-id=i-blahblah", "aws:instance-type=t2.small", "custom_tag=true"})
}

func TestFetchingTagsFromGCP(t *testing.T) {
	fetcher := &tagFetcher{
		gcpMetadata: func() (map[string]string, error) {
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

	tags := fetcher.Fetch(logger.Discard, FetchTagsConfig{
		Tags:              []string{"llamas", "rock"},
		TagsFromGCP:       true,
		TagsFromGCPLabels: true,
	})

	assert.ElementsMatch(t, tags,
		[]string{"llamas", "rock", "gcp:instance-id=my-instance", "gcp:zone=blah", "custom_tag=true"})
}
