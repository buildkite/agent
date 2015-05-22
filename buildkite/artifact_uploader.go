package buildkite

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"github.com/buildkite/agent/buildkite/logger"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ArtifactUploader struct {
	// The ID of the Job
	JobID string

	// The path of the uploads
	Paths string

	// Where we'll be uploading artifacts
	Destination string

	// The API used for communication
	API API
}

func (a *ArtifactUploader) Upload() error {
	// Create artifact structs for all the files we need to upload
	artifacts, err := a.collect()
	if err != nil {
		return err
	}

	if len(artifacts) == 0 {
		logger.Info("No files matched paths: %s", a.Paths)
	} else {
		logger.Info("Found %d files that match \"%s\"", len(artifacts), a.Paths)

		err := a.upload(artifacts)
		if err != nil {
			return err
		}
	}

	return nil
}

func (a *ArtifactUploader) collect() (artifacts []*Artifact, err error) {
	globs := strings.Split(a.Paths, ";")
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
				artifact, err := a.build(relativePath, absolutePath, glob)
				if err != nil {
					return nil, err
				}

				artifacts = append(artifacts, artifact)
			}
		}
	}

	return artifacts, nil
}

func (a *ArtifactUploader) build(relativePath string, absolutePath string, globPath string) (*Artifact, error) {
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
	artifact.API = a.API
	artifact.JobID = a.JobID
	artifact.State = "new"
	artifact.Path = relativePath
	artifact.AbsolutePath = absolutePath
	artifact.GlobPath = globPath
	artifact.FileSize = fileInfo.Size()
	artifact.Sha1Sum = checksum

	return artifact, nil
}

func (a *ArtifactUploader) upload(artifacts []*Artifact) error {
	var uploader Uploader

	// Determine what uploader to use
	if a.Destination != "" {
		if strings.HasPrefix(a.Destination, "s3://") {
			uploader = new(S3Uploader)
		} else {
			return errors.New("Unknown upload destination: " + a.Destination)
		}
	} else {
		uploader = new(FormUploader)
	}

	// Setup the uploader
	err := uploader.Setup(a.Destination)
	if err != nil {
		return err
	}

	// Set the URL's of the artifacts based on the uploader
	for _, artifact := range artifacts {
		artifact.URL = uploader.URL(artifact)
	}

	// Create artifacts on buildkite in batches to prevent timeouts with many artifacts
	var lenArtifacts = len(artifacts)
	var createdArtifacts = []*Artifact{}

	for i := 0; i < lenArtifacts; i += 100 {
		j := i + 100
		if lenArtifacts < j {
			j = lenArtifacts
		}

		someArtifacts := artifacts[i:j]

		someArtifactsCollection := ArtifactCollection{
			Artifacts: someArtifacts,
			API:       a.API,
			JobID:     a.JobID,
		}

		err := someArtifactsCollection.Create()
		if err != nil {
			return err
		}

		createdArtifacts = append(createdArtifacts, someArtifactsCollection.Artifacts...)
	}

	// Upload the artifacts by spinning up some routines
	var routines []chan string
	var concurrency int = 10

	logger.Debug("Spinning up %d concurrent threads for uploads", concurrency)

	count := 0
	for _, artifact := range createdArtifacts {
		// Create a channel and append it to the routines array. Once we've hit our
		// concurrency limit, we'll block until one finishes, then this loop will
		// startup up again.
		count++
		wait := make(chan string)
		go uploadRoutine(wait, artifact, uploader)
		routines = append(routines, wait)

		if count >= concurrency {
			logger.Debug("Maximum concurrent threads running. Waiting.")

			// Wait for all the routines to finish, then reset
			waitForUploadRoutines(routines)
			count = 0
			routines = routines[0:0]
		}
	}

	// Wait for any other routines to finish
	waitForUploadRoutines(routines)

	return nil
}

func uploadRoutine(quit chan string, artifact *Artifact, uploader Uploader) {
	// Show a nice message that we're starting to upload the file
	logger.Info("Uploading \"%s\" (%d bytes)", artifact.Path, artifact.FileSize)

	// Upload the artifact and then set the state depending on whether or not
	// it passed.
	err := uploader.Upload(artifact)
	if err != nil {
		artifact.State = "error"
		logger.Error("Error uploading artifact \"%s\": %s", artifact.Path, err)
	} else {
		artifact.State = "finished"
	}

	// Update the state of the artifact on Buildkite
	err = artifact.Update()
	if err != nil {
		logger.Error("Error marking artifact %s as uploaded: %s", artifact.Path, err)
	}

	// We can notify the channel that this routine has finished now
	quit <- "finished"
}

func waitForUploadRoutines(routines []chan string) {
	for _, r := range routines {
		<-r
	}
}
