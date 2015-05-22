package buildkite

import (
	"fmt"
	"mime"
	"path/filepath"
)

type Artifact struct {
	// The ID of the artifact
	ID string `json:"id,omitempty"`

	// The ID of the job
	JobID string

	// The current state of the artifact. Default is "new"
	State string `json:"state,omitempty"`

	// The relative path to the file
	Path string `json:"path"`

	// The absolute path path to the file
	AbsolutePath string `json:"absolute_path"`

	// The glob path that was used to identify this file
	GlobPath string `json:"glob_path"`

	// The size of the file
	FileSize int64 `json:"file_size"`

	// The sha1sum of the file
	Sha1Sum string `json:"sha1sum"`

	// Where we should upload the artifact to. If nil,
	// it will upload to Buildkite.
	URL string `json:"url,omitempty"`

	// When uploading artifacts to Buildkite, the API will return some
	// extra information on how/where to upload the file.
	Uploader struct {
		// Where/how to upload the file
		Action struct {
			// What the host to post to
			URL string `json:"url,omitempty"`

			// POST, PUT, GET, etc.
			Method string

			// What's the path at the URL we need to upload to
			Path string

			// What's the key of the file input named?
			FileInput string `json:"file_input"`
		}

		// Data that should be sent along with the upload
		Data map[string]string
	}

	// The API used for communication
	API API
}

func (a Artifact) String() string {
	return fmt.Sprintf("Artifact{ID: %s, Path: %s, URL: %s, AbsolutePath: %s, GlobPath: %s, FileSize: %d, Sha1Sum: %s}", a.ID, a.Path, a.URL, a.AbsolutePath, a.GlobPath, a.FileSize, a.Sha1Sum)
}

func (a *Artifact) Update() error {
	return a.API.Put("/jobs/"+a.JobID+"/artifacts/"+a.ID, &a, a)
}

func (a Artifact) MimeType() string {
	extension := filepath.Ext(a.Path)
	mimeType := mime.TypeByExtension(extension)

	if mimeType != "" {
		return mimeType
	} else {
		return "binary/octet-stream"
	}
}
