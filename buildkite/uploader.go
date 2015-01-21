package buildkite

type Uploader interface {
	// Called before anything happens.
	Setup(string) error

	// The Artifact.URL property is populated with what ever is returned
	// from this method prior to uploading.
	URL(*Artifact) string

	// The actual uploading of the file
	Upload(*Artifact) error
}
