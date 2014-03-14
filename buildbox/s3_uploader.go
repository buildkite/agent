package buildbox

import (
  "io/ioutil"
  "github.com/crowdmob/goamz/s3"
  "github.com/crowdmob/goamz/aws"
  "os"
  "fmt"
  "strings"
)

type S3Uploader struct {
  // The destination which includes the S3 bucket name
  // and the path.
  // s3://my-bucket-name/foo/bar
  Destination string

  // The S3 Bucket we're uploading these files to
  Bucket *s3.Bucket
}

func (u *S3Uploader) Setup(destination string) (error) {
  u.Destination = destination

  // Setup the AWS authentication
  auth, err := aws.EnvAuth()
  if err != nil {
    fmt.Printf("Error loading AWS credentials: %s", err)
    os.Exit(1)
  }

  // Find the bucket
  s3 := s3.New(auth, aws.USEast)
  bucket := s3.Bucket(u.bucketName())

  // If the list doesn't return an error, then we've got our
  // bucket
  _, err = bucket.List("", "", "", 0)
  if err != nil {
    return err
  }

  u.Bucket = bucket

  return nil
}

func (u *S3Uploader) URL(artifact *Artifact) (string) {
  return "http://" + u.bucketName() + ".s3.amazonaws.com/" + u.artifactPath(artifact)
}

func (u *S3Uploader) Upload(artifact *Artifact) (error) {
  Perms := s3.ACL("public-read")

  data, err := ioutil.ReadFile(artifact.AbsolutePath)
  if err != nil {
    return err
  }

  err = u.Bucket.Put(u.artifactPath(artifact), data, artifact.MimeType(), Perms, s3.Options{})
  if err != nil {
    return err
  }

  return nil
}

// func (u S3Uploader) Download(file string, bucket *s3.Bucket, path string) {
//   data, err := bucket.Get(path)
//   if err != nil {
//     panic(err.Error())
//   }
//   perms := os.FileMode(0644)
//
//   err = ioutil.WriteFile(file, data, perms)
//   if err != nil {
//     panic(err.Error())
//   }
// }

func (u *S3Uploader) artifactPath(artifact *Artifact) (string) {
  parts := []string{u.bucketPath(), artifact.Path}

  return strings.Join(parts, "/")
}

func (u *S3Uploader) bucketPath() string {
  return strings.Join(u.destinationParts()[1:len(u.destinationParts())], "/")
}

func (u *S3Uploader) bucketName() (string) {
  return u.destinationParts()[0]
}

func (u *S3Uploader) destinationParts() []string {
  trimmed_string := strings.TrimLeft(u.Destination, "s3://")

  return strings.Split(trimmed_string, "/")
}
