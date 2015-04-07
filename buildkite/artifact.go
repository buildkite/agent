package buildkite

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"github.com/buildkite/agent/buildkite/logger"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Artifact struct {
	// The ID of the artifact
	ID string `json:"id,omitempty"`

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
}

func (a Artifact) String() string {
	return fmt.Sprintf("Artifact{ID: %s, Path: %s, URL: %s, AbsolutePath: %s, GlobPath: %s, FileSize: %d, Sha1Sum: %s}", a.ID, a.Path, a.URL, a.AbsolutePath, a.GlobPath, a.FileSize, a.Sha1Sum)
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

func (c *Client) ArtifactUpdate(job *Job, artifact Artifact) (*Artifact, error) {
	// Create a new instance of a artifact that will be populated
	// with the updated data by the client
	var updatedArtifact Artifact

	// Return the job.
	return &updatedArtifact, c.Put(&updatedArtifact, "jobs/"+job.ID+"/artifacts/"+artifact.ID, artifact)
}

// Sends all the artifacts at once to the Buildkite Agent API. This will allow
// the UI to show what artifacts will be uploaded. Their state starts out as
// "new"
func (c *Client) CreateArtifacts(job *Job, artifacts []*Artifact) ([]Artifact, error) {
	var createdArtifacts []Artifact

	return createdArtifacts, c.Post(&createdArtifacts, "jobs/"+job.ID+"/artifacts", artifacts)
}

// Searches for artifacts on the build
func (c *Client) SearchArtifacts(buildId string, searchQuery string, jobQuery string, stateQuery string) ([]Artifact, error) {
	var foundArtifacts []Artifact

	return foundArtifacts, c.Get(&foundArtifacts, "builds/"+buildId+"/artifacts/search?query="+url.QueryEscape(searchQuery)+"&job="+url.QueryEscape(jobQuery)+"&state="+url.QueryEscape(stateQuery))
}

func CollectArtifacts(job *Job, artifactPaths string) (artifacts []*Artifact, err error) {
	globs := strings.Split(artifactPaths, ";")
	workingDirectory, _ := os.Getwd()

	for _, glob := range globs {
		glob = strings.TrimSpace(glob)

		if glob != "" {
			logger.Debug("Globbing %s for %s", workingDirectory, glob)

			files, err := Glob(workingDirectory, glob)
			if err != nil {
				return nil, err
			}

			for _, file := range files {
				// Generate an absolute path for the artifact
				absolutePath, err := filepath.Abs(file)
				if err != nil {
					return nil, err
				}

				fileInfo, err := os.Stat(absolutePath)
				if fileInfo.IsDir() {
					logger.Debug("Skipping directory %s", file)
					continue
				}

				// Create a relative path (from the workingDirectory) to the artifact, by removing the
				// first part of the absolutePath that is the workingDirectory.
				relativePath := strings.Replace(absolutePath, workingDirectory, "", 1)

				// Ensure the relativePath doesn't have a file seperator "/" as the first character
				relativePath = strings.TrimPrefix(relativePath, string(os.PathSeparator))

				// Build an artifact object using the paths we have.
				artifact, err := BuildArtifact(relativePath, absolutePath, glob)
				if err != nil {
					return nil, err
				}

				artifacts = append(artifacts, artifact)
			}
		}
	}

	return artifacts, nil
}

func BuildArtifact(relativePath string, absolutePath string, globPath string) (*Artifact, error) {
	// Temporarily open the file to get it's size
	file, err := os.Open(absolutePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Grab it's file info (which includes it's file size)
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	// Generate a sha1 checksum for the file
	hash := sha1.New()
	io.Copy(hash, file)
	checksum := fmt.Sprintf("%x", hash.Sum(nil))

	// Create our new artifact data structure
	artifact := new(Artifact)
	artifact.State = "new"
	artifact.Path = relativePath
	artifact.AbsolutePath = absolutePath
	artifact.GlobPath = globPath
	artifact.FileSize = fileInfo.Size()
	artifact.Sha1Sum = checksum

	return artifact, nil
}

func UploadArtifacts(client Client, job *Job, artifacts []*Artifact, destination string) error {
	var uploader Uploader

	// Determine what uploader to use
	if destination != "" {
		if strings.HasPrefix(destination, "s3://") {
			uploader = new(S3Uploader)
		} else {
			return errors.New("Unknown upload destination: " + destination)
		}
	} else {
		uploader = new(FormUploader)
	}

	// Setup the uploader
	err := uploader.Setup(destination)
	if err != nil {
		return err
	}

	// Set the URL's of the artifacts based on the uploader
	for _, artifact := range artifacts {
		artifact.URL = uploader.URL(artifact)
	}

	// Create artifacts on buildkite in batches to prevent timeouts with many artifacts
	var lenArtifacts = len(artifacts)
	var createdArtifacts = []Artifact{}

	for i := 0; i < lenArtifacts; i += 100 {
		j := i + 100
		if lenArtifacts < j {
			j = lenArtifacts
		}

		someArtifacts := artifacts[i:j]

		someCreatedArtifacts, err := client.CreateArtifacts(job, someArtifacts)
		if err != nil {
			return err
		}

		createdArtifacts = append(createdArtifacts, someCreatedArtifacts...)
	}

	// Upload the artifacts by spinning up some routines
	var routines []chan string
	var concurrency int = 10

	logger.Debug("Spinning up %d concurrent threads for uploads", concurrency)

	count := 0
	for _, artifact := range createdArtifacts {
		// Create a channel and apend it to the routines array. Once we've hit our
		// concurrency limit, we'll block until one finishes, then this loop will
		// startup up again.
		count++
		wait := make(chan string)
		go uploadRoutine(wait, client, job, artifact, uploader)
		routines = append(routines, wait)

		if count >= concurrency {
			logger.Debug("Maxiumum concurrent threads running. Waiting.")

			// Wait for all the routines to finish, then reset
			waitForRoutines(routines)
			count = 0
			routines = routines[0:0]
		}
	}

	// Wait for any other routines to finish
	waitForRoutines(routines)

	return nil
}

func uploadRoutine(quit chan string, client Client, job *Job, artifact Artifact, uploader Uploader) {
	// Show a nice message that we're starting to upload the file
	logger.Info("Uploading \"%s\" (%d bytes)", artifact.Path, artifact.FileSize)

	// Upload the artifact and then set the state depending on whether or not
	// it passed.
	err := uploader.Upload(&artifact)
	if err != nil {
		artifact.State = "error"
		logger.Error("Error uploading artifact \"%s\": %s", artifact.Path, err)
	} else {
		artifact.State = "finished"
	}

	// Update the state of the artifact on Buildkite
	_, err = client.ArtifactUpdate(job, artifact)
	if err != nil {
		logger.Error("Error marking artifact %s as uploaded: %s", artifact.Path, err)
	}

	// We can notify the channel that this routine has finished now
	quit <- "finished"
}

func DownloadArtifacts(artifacts []Artifact, destination string) error {
	// Download the files by spinning up some routines
	var routines []chan string
	var concurrency int = 10

	logger.Debug("Spinning up %d concurrent threads for downloads", concurrency)

	count := 0
	for _, artifact := range artifacts {
		// Create a channel and apend it to the routines array. Once we've hit our
		// concurrency limit, we'll block until one finishes, then this loop will
		// startup up again.
		count++
		wait := make(chan string)
		download := Download{URL: artifact.URL, Path: artifact.Path, Destination: destination}
		go StartDownload(wait, download)
		routines = append(routines, wait)

		if count >= concurrency {
			logger.Debug("Maxiumum concurrent threads running. Waiting.")

			// Wait for all the routines to finish, then reset
			waitForRoutines(routines)
			count = 0
			routines = routines[0:0]
		}
	}

	// Wait for any other routines to finish
	waitForRoutines(routines)

	return nil
}

func waitForRoutines(routines []chan string) {
	for _, r := range routines {
		<-r
	}
}
