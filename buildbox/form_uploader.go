package buildbox

import (
  "bytes"
  "fmt"
  "io"
  "mime/multipart"
  "net/http"
  _ "crypto/sha512" // import sha512 to make sha512 ssl certs work
  // "net/http/httputil"
  "os"
  "path/filepath"
  "net/url"
  "errors"
)

type FormUploader struct {
}

func (u *FormUploader) Setup(destination string) (error) {
  return nil
}

// The FormUploader doens't specify a URL, as one is provided by Buildbox
// after uploading
func (u *FormUploader) URL(artifact *Artifact) (string) {
  return ""
}

func (u *FormUploader) Upload(artifact *Artifact) (error) {
  // Create a HTTP request for uploading the file
  request, err := createUploadRequest(artifact)
  if err != nil {
    return err
  }

  // dump, err := httputil.DumpRequest(request, true)
  // if err != nil {
  //   fmt.Println(err)
  // } else {
  //   os.Stderr.Write(dump)
  //   os.Stderr.Write([]byte{'\n'})
  // }

  // Perform the request
  client := &http.Client{}
  response, err := client.Do(request)

  // Check for errors
  if err != nil {
    return err
  } else {
    // Be sure to close the response body at the end of
    // this function
    defer response.Body.Close()

    // dump, err := httputil.DumpResponse(response, true)
    // if err != nil {
    //   fmt.Println(err)
    // } else {
    //   os.Stderr.Write(dump)
    //   os.Stderr.Write([]byte{'\n'})
    // }

    if response.StatusCode/100 != 2 {
      body := &bytes.Buffer{}
      _, err := body.ReadFrom(response.Body)
      if err != nil {
        return err
      }

      // Return a custom error with the response body from the page
      message := fmt.Sprintf("%s (%d)", body, response.StatusCode)
      return errors.New(message)
    }
  }

  return nil
}

// Creates a new file upload http request with optional extra params
func createUploadRequest(artifact *Artifact) (*http.Request, error) {
  file, err := os.Open(artifact.AbsolutePath)
  if err != nil {
    return nil, err
  }
  defer file.Close()

  body := &bytes.Buffer{}
  writer := multipart.NewWriter(body)

  // Set the post data for the request
  for key, val := range artifact.Uploader.Data {
    err = writer.WriteField(key, val)
    if err != nil {
      return nil, err
    }
  }

  // It's important that we add the form field last because when uploading to an S3
  // form, they are really nit-picky about the field order, and the file needs to be
  // the last one other it doesn't work.
  part, err := writer.CreateFormFile(artifact.Uploader.Action.FileInput, filepath.Base(artifact.AbsolutePath))
  if err != nil {
    return nil, err
  }
  _, err = io.Copy(part, file)

  err = writer.Close()
  if err != nil {
    return nil, err
  }

  // Create the URL that we'll send data to
  uri, err := url.Parse(artifact.Uploader.Action.URL)
  uri.Path = artifact.Uploader.Action.Path

  // Create the request
  req, err := http.NewRequest(artifact.Uploader.Action.Method, uri.String(), body)
  if err != nil {
    return nil, err
  }

  // Finally add the multipart content type to the request
  req.Header.Add("Content-Type", writer.FormDataContentType())

  return req, nil
}
