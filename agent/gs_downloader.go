package agent

import (
	"errors"
	"fmt"

	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	storage "google.golang.org/api/storage/v1"
)

type GSDownloader struct {
	// The URL
	URL string

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

	// We can now cheat and pass the URL onto our regular downloader
	return Download{
		Client:      *client,
		URL:         d.URL,
		Path:        d.Path,
		Destination: d.Destination,
		Retries:     d.Retries,
		DebugHTTP:   d.DebugHTTP,
	}.Start()
}
