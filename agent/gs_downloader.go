package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/buildkite/agent/logger"
	"golang.org/x/oauth2/google"
	storage "google.golang.org/api/storage/v1"
)

type GSDownloader struct {
	// The logger instance to use
	Logger *logger.Logger

	// The name of the bucket
	Bucket string

	// The root directory of the download
	Destination string

	// The relative path that should be preserved in the download folder,
	// also it's location in the bucket
	Path string

	// How many times should it retry the download before giving up
	Retries int

	// If failed responses should be dumped to the log
	DebugHTTP bool
}

func (d GSDownloader) Start() error {
	client, err := google.DefaultClient(context.Background(), storage.DevstorageReadOnlyScope)
	if err != nil {
		return errors.New(fmt.Sprintf("Error creating Google Cloud Storage client: %v", err))
	}

	url := "https://www.googleapis.com/storage/v1/b/" + d.BucketName() + "/o/" + escape(d.BucketFileLocation()) + "?alt=media"

	// We can now cheat and pass the URL onto our regular downloader
	return Download{
		Logger:      d.Logger,
		Client:      *client,
		URL:         url,
		Path:        d.Path,
		Destination: d.Destination,
		Retries:     d.Retries,
		DebugHTTP:   d.DebugHTTP,
	}.Start()
}

func (d GSDownloader) BucketFileLocation() string {
	if d.BucketPath() != "" {
		return strings.TrimSuffix(d.BucketPath(), "/") + "/" + strings.TrimPrefix(d.Path, "/")
	} else {
		return d.Path
	}
}

func (d GSDownloader) BucketPath() string {
	return strings.Join(d.destinationParts()[1:len(d.destinationParts())], "/")
}

func (d GSDownloader) BucketName() string {
	return d.destinationParts()[0]
}

func (d GSDownloader) destinationParts() []string {
	trimmed := strings.TrimPrefix(d.Bucket, "gs://")

	return strings.Split(trimmed, "/")
}

func escape(s string) string {
	// See https://golang.org/src/net/url/url.go
	hexCount := 0
	for i := 0; i < len(s); i++ {
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
	for i := 0; i < len(s); i++ {
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
