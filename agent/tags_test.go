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
	l := logger.NewTextLogger()

	tags := FetchTags(l, FetchTagsConfig{
		Tags: []string{"llamas", "rock"},
	})

	if !reflect.DeepEqual(tags, []string{"llamas", "rock"}) {
		t.Fatalf("bad tags: %#v", tags)
	}

	t.Logf("Tags: %#v", tags)
}

func TestFetchingTagsWithHostTags(t *testing.T) {
	l := logger.NewTextLogger()

	tags := FetchTags(l, FetchTagsConfig{
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

func TestFetchingTagsWithEC2Metadata(t *testing.T) {
	l := logger.NewTextLogger()

	tags := FetchTags(l, FetchTagsConfig{
		Tags:        []string{"llamas", "rock"},
		TagsFromEC2: true,
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
