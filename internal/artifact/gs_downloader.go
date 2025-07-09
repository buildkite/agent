package artifact

import (
	"context"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v3/logger"
	storage "google.golang.org/api/storage/v1"
)

type GSDownloaderConfig struct {
	// The name of the bucket
	Bucket string

	// The root directory of the download
	Destination string

	// The relative path that should be preserved in the download folder,
	// also its location in the bucket
	Path string

	// How many times should it retry the download before giving up
	Retries int

	// If failed responses should be dumped to the log
	DebugHTTP bool
	TraceHTTP bool
}

type GSDownloader struct {
	// The config for the downloader
	conf GSDownloaderConfig

	// The logger instance to use
	logger logger.Logger
}

func NewGSDownloader(l logger.Logger, c GSDownloaderConfig) *GSDownloader {
	return &GSDownloader{
		logger: l,
		conf:   c,
	}
}

func (d GSDownloader) Start(ctx context.Context) error {
	client, err := newGoogleClient(ctx, storage.DevstorageReadOnlyScope)
	if err != nil {
		return fmt.Errorf("creating Google Cloud Storage client: %w", err)
	}

	url := "https://www.googleapis.com/storage/v1/b/" + d.BucketName() + "/o/" + escape(d.BucketFileLocation()) + "?alt=media"

	// We can now cheat and pass the URL onto our regular downloader
	return NewDownload(d.logger, client, DownloadConfig{
		URL:         url,
		Path:        d.conf.Path,
		Destination: d.conf.Destination,
		Retries:     d.conf.Retries,
		DebugHTTP:   d.conf.DebugHTTP,
		TraceHTTP:   d.conf.TraceHTTP,
	}).Start(ctx)
}

func (d GSDownloader) BucketFileLocation() string {
	if d.BucketPath() != "" {
		return strings.TrimSuffix(d.BucketPath(), "/") + "/" + strings.TrimPrefix(d.conf.Path, "/")
	} else {
		return d.conf.Path
	}
}

func (d GSDownloader) BucketPath() string {
	return strings.Join(d.destinationParts()[1:len(d.destinationParts())], "/")
}

func (d GSDownloader) BucketName() string {
	return d.destinationParts()[0]
}

func (d GSDownloader) destinationParts() []string {
	trimmed := strings.TrimPrefix(d.conf.Bucket, "gs://")

	return strings.Split(trimmed, "/")
}

func escape(s string) string {
	// See https://golang.org/src/net/url/url.go
	hexCount := 0
	for i := range len(s) {
		c := s[i]
		if shouldEscape(c) {
			hexCount++
		}
	}

	if hexCount == 0 {
		return s
	}

	t := make([]byte, len(s)+2*hexCount)
	j := 0
	for i := range len(s) {
		switch c := s[i]; {
		case shouldEscape(c):
			t[j] = '%'
			t[j+1] = "0123456789ABCDEF"[c>>4]
			t[j+2] = "0123456789ABCDEF"[c&15]
			j += 3
		default:
			t[j] = s[i]
			j++
		}
	}
	return string(t)
}

func shouldEscape(c byte) bool {
	// See https://cloud.google.com/storage/docs/json_api/#encoding
	if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' {
		return false
	}
	switch c {
	case '-', '.', '_', '~', '!', '$', '&', '\'', '(', ')', '*', '+', ',', ';', '=', ':', '@':
		return false
	}
	return true
}
