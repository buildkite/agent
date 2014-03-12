package buildbox

import (
  "fmt"
  "os"
  "strings"
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
}

func (a Artifact) String() string {
  return fmt.Sprintf("Artifact{ID: %s, Path: %s, AbsolutePath: %s, GlobPath: %s, FileSize: %d}", a.ID, a.Path, a.AbsolutePath, a.GlobPath, a.FileSize)
}

func (a Artifact) Update(client *Client, job *Job, artifact *Artifact) (*Artifact, error) {
  // Create a new instance of a artifact that will be populated
  // with the updated data by the client
  var updatedArtifact Artifact

  // Return the job.
  return &updatedArtifact, client.Put(&updatedArtifact, "jobs/" + job.ID + "/artifacts/" + artifact.ID, artifact)
}

func CreateArtifacts(client Client, job *Job, artifacts []*Artifact) ([]Artifact, error) {
  var updatedArtifacts []Artifact

  return updatedArtifacts, client.Post(&updatedArtifacts, "jobs/" + job.ID + "/artifacts", artifacts)
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

  return &Artifact{"", path, absolutePath, globPath, fileInfo.Size()}, nil
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
