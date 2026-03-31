package agent

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/roko"
	"github.com/denisbrodbeck/machineid"
)

type FetchTagsConfig struct {
	Tags []string

	TagsFromK8s               bool
	TagsFromEC2MetaData       bool
	TagsFromEC2MetaDataPaths  []string
	TagsFromEC2Tags           bool
	TagsFromECSMetaData       bool
	TagsFromGCPMetaData       bool
	TagsFromGCPMetaDataPaths  []string
	TagsFromGCPLabels         bool
	TagsFromHost              bool
	FailOnMissingTags         bool
	WaitForEC2TagsTimeout     time.Duration
	WaitForEC2MetaDataTimeout time.Duration
	WaitForECSMetaDataTimeout time.Duration
	WaitForGCPLabelsTimeout   time.Duration
}

// FetchTags loads tags from a variety of sources.
// If conf.FailOnMissingTags is true, an error is returned when any enabled
// cloud tag source (EC2, ECS, GCP) fails to provide tags.
func FetchTags(ctx context.Context, l logger.Logger, conf FetchTagsConfig) ([]string, error) {
	f := &tagFetcher{
		k8s: func() (map[string]string, error) {
			return K8sTagsFromEnv(os.Environ())
		},
		ec2MetaDataDefault: func() (map[string]string, error) {
			return EC2MetaData{}.Get(ctx)
		},
		ec2MetaDataPaths: func(paths map[string]string) (map[string]string, error) {
			return EC2MetaData{}.GetPaths(ctx, paths)
		},
		ec2Tags: func() (map[string]string, error) {
			return EC2Tags{}.Get(ctx)
		},
		ecsMetaDataDefault: func() (map[string]string, error) {
			return ECSMetadata{}.Get(ctx)
		},
		gcpMetaDataDefault: func() (map[string]string, error) {
			return GCPMetaData{}.Get(ctx)
		},
		gcpMetaDataPaths: func(paths map[string]string) (map[string]string, error) {
			return GCPMetaData{}.GetPaths(ctx, paths)
		},
		gcpLabels: func() (map[string]string, error) {
			return GCPLabels{}.Get(ctx)
		},
	}
	return f.Fetch(ctx, l, conf)
}

type tagFetcher struct {
	k8s                func() (map[string]string, error)
	ec2MetaDataDefault func() (map[string]string, error)
	ec2MetaDataPaths   func(map[string]string) (map[string]string, error)
	ec2Tags            func() (map[string]string, error)
	ecsMetaDataDefault func() (map[string]string, error)
	gcpMetaDataDefault func() (map[string]string, error)
	gcpMetaDataPaths   func(map[string]string) (map[string]string, error)
	gcpLabels          func() (map[string]string, error)
}

func (t *tagFetcher) Fetch(ctx context.Context, l logger.Logger, conf FetchTagsConfig) ([]string, error) {
	tags := conf.Tags

	if conf.TagsFromK8s {
		k8sTags, err := t.k8s()
		if err != nil {
			l.Warnf("Could not fetch tags from k8s: %s", err)
		}
		for tag, value := range k8sTags {
			tags = append(tags, fmt.Sprintf("%s=%s", tag, value))
		}
	}

	// Load tags from host
	if conf.TagsFromHost {
		hostname, err := os.Hostname()
		if err != nil {
			l.Warnf("Failed to find hostname: %v", err)
		}

		tags = append(tags,
			fmt.Sprintf("hostname=%s", hostname),
			fmt.Sprintf("os=%s", runtime.GOOS),
			fmt.Sprintf("arch=%s", runtime.GOARCH),
		)

		machineID, _ := machineid.ProtectedID("buildkite-agent")
		if machineID != "" {
			tags = append(tags, fmt.Sprintf("machine-id=%s", machineID))
		}
	}

	// Attempt to add the default EC2 meta-data tags
	if conf.TagsFromEC2MetaData {
		l.Infof("Fetching EC2 meta-data...")

		err := roko.NewRetrier(
			roko.WithMaxAttempts(5),
			roko.WithStrategy(roko.Constant(conf.WaitForEC2MetaDataTimeout/5)),
			roko.WithJitter(),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			ec2Tags, err := t.ec2MetaDataDefault()
			if err != nil {
				l.Warnf("%s (%s)", err, r)
			} else {
				l.Infof("Successfully fetched EC2 meta-data")
				for tag, value := range ec2Tags {
					tags = append(tags, fmt.Sprintf("%s=%s", tag, value))
				}
				r.Break()
			}

			return err
		})
		if err != nil {
			if conf.FailOnMissingTags {
				return nil, fmt.Errorf("failed to fetch EC2 meta-data: %w", err)
			}
			l.Errorf(fmt.Sprintf("Failed to fetch EC2 meta-data: %s", err.Error()))
		}
	}

	// Attempt to add the EC2 meta-data fetched from user-default path suffixes
	if len(conf.TagsFromEC2MetaDataPaths) > 0 {
		paths, err := parseTagValuePathPairs(conf.TagsFromEC2MetaDataPaths)
		if err != nil {
			l.Errorf(fmt.Sprintf("Error parsing meta-data tag and path pairs: %s", err.Error()))
		}

		ec2Tags, err := t.ec2MetaDataPaths(paths)
		if err != nil {
			if conf.FailOnMissingTags {
				return nil, fmt.Errorf("failed to fetch EC2 meta-data from paths: %w", err)
			}
			l.Errorf(fmt.Sprintf("Failed to fetch EC2 meta-data: %s", err.Error()))
		} else {
			for tag, value := range ec2Tags {
				tags = append(tags, fmt.Sprintf("%s=%s", tag, value))
			}
		}
	}

	// Attempt to add the EC2 tags
	if conf.TagsFromEC2Tags {
		l.Infof("Fetching EC2 tags...")

		err := roko.NewRetrier(
			roko.WithMaxAttempts(5),
			roko.WithStrategy(roko.Constant(conf.WaitForEC2TagsTimeout/5)),
			roko.WithJitter(),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			ec2Tags, err := t.ec2Tags()
			// EC2 tags are apparently "eventually consistent" and sometimes take several seconds
			// to be applied to instances. This error will cause retries.
			if err == nil && len(ec2Tags) == 0 {
				err = errors.New("EC2 tags are empty")
			}
			if err != nil {
				l.Warnf("%s (%s)", err, r)
			} else {
				l.Infof("Successfully fetched EC2 tags")
				for tag, value := range ec2Tags {
					tags = append(tags, fmt.Sprintf("%s=%s", tag, value))
				}
				r.Break()
			}
			return err
		})
		if err != nil {
			if conf.FailOnMissingTags {
				return nil, fmt.Errorf("failed to fetch EC2 tags: %w", err)
			}
			l.Errorf(fmt.Sprintf("Failed to find EC2 tags: %s", err.Error()))
		}
	}

	// Attempt to add the default ECS meta-data tags
	if conf.TagsFromECSMetaData {
		l.Infof("Fetching ECS meta-data...")

		err := roko.NewRetrier(
			roko.WithMaxAttempts(5),
			roko.WithStrategy(roko.Constant(conf.WaitForECSMetaDataTimeout/5)),
			roko.WithJitter(),
		).Do(func(r *roko.Retrier) error {
			ecsTags, err := t.ecsMetaDataDefault()
			if err != nil {
				l.Warnf("%s (%s)", err, r)
			} else {
				l.Infof("Successfully fetched ECS meta-data")
				for tag, value := range ecsTags {
					tags = append(tags, fmt.Sprintf("%s=%s", tag, value))
				}
				r.Break()
			}

			return err
		})
		if err != nil {
			if conf.FailOnMissingTags {
				return nil, fmt.Errorf("failed to fetch ECS meta-data: %w", err)
			}
			l.Errorf(fmt.Sprintf("Failed to fetch ECS meta-data: %s", err.Error()))
		}
	}

	// Attempt to add the default GCP meta-data tags
	if conf.TagsFromGCPMetaData {
		l.Infof("Fetching GCP meta-data...")

		err := roko.NewRetrier(
			roko.WithMaxAttempts(5),
			roko.WithStrategy(roko.Constant(1*time.Second)),
			roko.WithJitter(),
		).DoWithContext(ctx, func(_ *roko.Retrier) error {
			gcpTags, err := t.gcpMetaDataDefault()
			if err != nil {
				// Don't blow up if we can't find them, just show a nasty error.
				l.Errorf(fmt.Sprintf("Failed to fetch Google Cloud meta-data: %s", err.Error()))
				return err
			}

			for tag, value := range gcpTags {
				tags = append(tags, fmt.Sprintf("%s=%s", tag, value))
			}

			return nil
		})
		if err != nil {
			if conf.FailOnMissingTags {
				return nil, fmt.Errorf("failed to fetch GCP meta-data: %w", err)
			}
			l.Errorf(fmt.Sprintf("Failed to fetch GCP meta-data: %s", err.Error()))
		}
	}

	// Attempt to add the Google Cloud meta-data
	if len(conf.TagsFromGCPMetaDataPaths) > 0 {
		paths, err := parseTagValuePathPairs(conf.TagsFromGCPMetaDataPaths)
		if err != nil {
			l.Errorf(fmt.Sprintf("Error parsing meta-data tag and path pairs: %s", err.Error()))
		}

		gcpTags, err := t.gcpMetaDataPaths(paths)
		if err != nil {
			if conf.FailOnMissingTags {
				return nil, fmt.Errorf("failed to fetch GCP meta-data from paths: %w", err)
			}
			l.Errorf(fmt.Sprintf("Failed to fetch Google Cloud meta-data: %s", err.Error()))
		} else {
			for tag, value := range gcpTags {
				tags = append(tags, fmt.Sprintf("%s=%s", tag, value))
			}
		}
	}

	// Attempt to add the Google Compute instance labels
	if conf.TagsFromGCPLabels {
		l.Infof("Fetching GCP instance labels...")
		err := roko.NewRetrier(
			roko.WithMaxAttempts(5),
			roko.WithStrategy(roko.Constant(conf.WaitForGCPLabelsTimeout/5)),
			roko.WithJitter(),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			labels, err := t.gcpLabels()
			if err == nil && len(labels) == 0 {
				err = errors.New("GCP instance labels are empty")
			}
			if err != nil {
				l.Warnf("%s (%s)", err, r)
			} else {
				l.Infof("Successfully fetched GCP instance labels")
				for label, value := range labels {
					tags = append(tags, fmt.Sprintf("%s=%s", label, value))
				}
				r.Break()
			}
			return err
		})
		if err != nil {
			if conf.FailOnMissingTags {
				return nil, fmt.Errorf("failed to fetch GCP instance labels: %w", err)
			}
			l.Errorf(fmt.Sprintf("Failed to find GCP instance labels: %s", err.Error()))
		}
	}

	return tags, nil
}

func parseTagValuePathPairs(paths []string) (map[string]string, error) {
	result := make(map[string]string)

	for _, pair := range paths {
		// Sanity check the format of each pair to ensure that it’s parseable
		index := strings.LastIndex(pair, "=")
		if index == -1 || index == 0 || index == len(pair)-1 {
			return result, fmt.Errorf("%q cannot be parsed, format should be `tag=metadata/path`", pair)
		}

		x := strings.Split(pair, "=")
		key := strings.ToLower(strings.TrimSpace(x[0]))

		uri, err := url.Parse(x[1])
		if err != nil {
			return result, err
		}

		result[key] = strings.TrimPrefix(uri.Path, "/")
	}

	return result, nil
}
