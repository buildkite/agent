package metrics

import (
	"regexp"
	"sort"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/buildkite/agent/logger"
)

type Collector struct {
	Datadog     bool
	DatadogHost string

	client *statsd.Client
}

func (c *Collector) Start() error {
	if c.Datadog {
		logger.Info("Starting datadog metrics collection to %s", c.DatadogHost)

		var err error
		c.client, err = statsd.New(c.DatadogHost)
		if err != nil {
			return err
		}

		c.client.Namespace = "buildkite."
	}
	return nil
}

func (c *Collector) Stop() error {
	if c.Datadog && c.client != nil {
		logger.Info("Stopping metrics collection")
		return c.client.Close()
	}
	return nil
}

func (c *Collector) Scope(tags Tags) *Scope {
	return &Scope{
		Tags: tags,
		c:    c,
	}
}

type Scope struct {
	Tags Tags
	c    *Collector
}

// Timing sends timing information in milliseconds.
func (s *Scope) Timing(name string, value time.Duration, tags ...Tags) {
	if s.c.client == nil {
		return
	}

	mergedTags := s.mergeTags(tags...).StringSlice()
	logger.Debug("Metrics timing %s=%v %v", name, value, mergedTags)

	if err := s.c.client.Timing(name, value, mergedTags, 1); err != nil {
		logger.Error("Metrics timing failed: %v", err)
	}
}

// Count tracks how many times something happened per second.
func (s *Scope) Count(name string, value int64, tags ...Tags) {
	if s.c.client == nil {
		return
	}

	mergedTags := s.mergeTags(tags...).StringSlice()
	logger.Debug("Metrics count %s=%v %v", name, value, mergedTags)

	if err := s.c.client.Count(name, value, mergedTags, 1); err != nil {
		logger.Error("Metrics count failed: %v", err)
	}
}

func (s *Scope) mergeTags(tagsSlice ...Tags) Tags {
	merged := Tags{}
	for k, v := range s.Tags {
		merged[formatName(k)] = formatName(v)
	}
	for _, tags := range tagsSlice {
		for k, v := range tags {
			merged[formatName(k)] = formatName(v)
		}
	}
	return merged
}

type Tags map[string]string

func (tags Tags) StringSlice() []string {
	var stringSlice []string
	for k, v := range tags {
		if k != "" && v != "" {
			stringSlice = append(stringSlice, formatName(k)+":"+formatName(v))
		}
	}
	sort.Strings(stringSlice)
	return stringSlice
}

// Datadog allows '.', '_' and alphas only.
// If we don't validate this here then the datadog error logs can fill up disk really quickly
var nameRegex = regexp.MustCompile(`[^\._a-zA-Z0-9]+`)

func formatName(name string) string {
	return nameRegex.ReplaceAllString(name, "_")
}
