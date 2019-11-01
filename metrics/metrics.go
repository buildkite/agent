package metrics

import (
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/buildkite/agent/v3/logger"
)

const (
	// Number of statsd commands that are buffered before
	// being sent to statsd
	statsdBufferLen = 10

	// The default port for dogstatsd
	defaultDogStatsdPort = 8125
)

type Collector struct {
	config CollectorConfig
	logger logger.Logger
	client *statsd.Client
}

type CollectorConfig struct {
	Datadog     bool
	DatadogHost string
}

func NewCollector(l logger.Logger, c CollectorConfig) *Collector {
	return &Collector{
		config: c,
		logger: l,
	}
}

var portSuffixRegexp = regexp.MustCompile(`:\d+$`)

func (c *Collector) Start() error {
	if c.config.Datadog {
		if !portSuffixRegexp.MatchString(c.config.DatadogHost) {
			c.config.DatadogHost += fmt.Sprintf(":%d", defaultDogStatsdPort)
		}

		c.logger.Info("Starting datadog metrics collection to %s", c.config.DatadogHost)

		var err error
		c.client, err = statsd.NewBuffered(c.config.DatadogHost, statsdBufferLen)
		if err != nil {
			return err
		}

		c.client.Namespace = "buildkite."
	}
	return nil
}

func (c *Collector) Stop() error {
	if c.config.Datadog && c.client != nil {
		c.logger.Info("Stopping metrics collection")
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
	s.c.logger.Debug("Metrics timing %s=%v %v", name, value, mergedTags)

	if err := s.c.client.Timing(name, value, mergedTags, 1); err != nil {
		s.c.logger.Error("Metrics timing failed: %v", err)
	}
}

// With returns a scope with more tags added
func (s *Scope) With(tags Tags) *Scope {
	return &Scope{
		Tags: s.mergeTags(tags),
		c:    s.c,
	}
}

// Count tracks how many times something happened per second.
func (s *Scope) Count(name string, value int64, tags ...Tags) {
	if s.c.client == nil {
		return
	}

	mergedTags := s.mergeTags(tags...).StringSlice()
	s.c.logger.Debug("Metrics count %s=%v %v", name, value, mergedTags)

	if err := s.c.client.Count(name, value, mergedTags, 1); err != nil {
		s.c.logger.Error("Metrics count failed: %v", err)
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
