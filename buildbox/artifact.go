package buildbox

import (
  "fmt"
  "os"
  "strings"
  "time"
  "log"
  "path/filepath"
)

type Artifact struct {
  // The ID of the artifact
  ID string `json:"id,omitempty"`

  // The relative path to the file
  Path string `json:"path,omitempty"`

  // The absolute path path to the file
  AbsolutePath string `json:"absolute_path,omitempty"`

  // The glob path that was used to identify this file
  GlobPath string `json:"glob_path,omitempty"`

  // The size of the file
  FileSize int64 `json:"file_size,omitempty"`

  // Where we should upload the artifact to. If nil,
  // it will upload to Buildbox.
  URL string `json:"url,omitempty"`
}

func (a Artifact) String() string {
  return fmt.Sprintf("Artifact{ID: %s, Path: %s, URL: %s, AbsolutePath: %s, GlobPath: %s, FileSize: %d}", a.ID, a.Path, a.URL, a.AbsolutePath, a.GlobPath, a.FileSize)
}

func (a Artifact) Update(client *Client, job *Job, artifact *Artifact) (*Artifact, error) {
  // Create a new instance of a artifact that will be populated
  // with the updated data by the client
  var updatedArtifact Artifact

  // Return the job.
  return &updatedArtifact, client.Put(&updatedArtifact, "jobs/" + job.ID + "/artifacts/" + artifact.ID, artifact)
}

func CollectArtifacts(job *Job, artifactPaths string) (artifacts []*Artifact, err error) {
  globs := strings.Split(artifactPaths, ";")

  for _, glob := range globs {
    files, err := filepath.Glob(glob)
    if err != nil {
      return nil, err
    }

    for _, file := range files {
      absolutePath, err := filepath.Abs(file)
      if err != nil {
        return nil, err
      }

      artifact, err := BuildArtifact(file, absolutePath, glob)
      if err != nil {
        return nil, err
      }

      artifacts = append(artifacts, artifact)
    }
  }

  return artifacts, nil
}

func BuildArtifact(path string, absolutePath string, globPath string) (*Artifact, error) {
  // Temporarily open the file to get it's size
  file, err := os.Open(path)
  if err != nil {
    return nil, err
  }
  defer file.Close()

  // Grab it's file info (which includes it's file size)
  fileInfo, err := file.Stat()
  if err != nil {
    return nil, err
  }

  return &Artifact{"", path, absolutePath, globPath, fileInfo.Size(), ""}, nil
}

func CreateArtifacts(client Client, job *Job, artifacts []*Artifact) ([]Artifact, error) {
  var updatedArtifacts []Artifact

  return updatedArtifacts, client.Post(&updatedArtifacts, "jobs/" + job.ID + "/artifacts", artifacts)
}

func UploadArtifacts(client Client, job *Job, artifacts []*Artifact) (error) {
  // Create artifacts on buildbox
  createdArtifacts, err := CreateArtifacts(client, job, artifacts)
  if err != nil {
    return err
  }

  // Upload the artifacts by spinning up some routines
  var routines []chan string
  var concurrency int = 20

  count := 0
  for _, artifact := range createdArtifacts {
    log.Printf("Uploading `%s` to `%s`\n", artifact.Path, artifact.URL)

    // Create a channel and apend it to the routines array. Once we've hit our
    // concurrency limit, we'll block until one finishes, then this loop will
    // startup up again.
    count++
    wait := make(chan string)
    go uploadRoutine(wait, artifact)
    routines = append(routines, wait)

    if count >= concurrency {
      // fmt.Printf("Maxiumum concurrent threads running. Waiting.\n")
      // Wait for all the routines to finish, then reset
      waitForRoutines(routines)
      count = 0
      routines = routines[0:0]
    }
  }

  return nil
}

func uploadRoutine(quit chan string, artifact Artifact) {
  idleSeconds := 5
  sleepTime := time.Duration(idleSeconds * 1000) * time.Millisecond
  time.Sleep(sleepTime)
  quit <- "finished"
}

func waitForRoutines(routines []chan string) {
  for _, r := range routines {
    <-r
  }
}
