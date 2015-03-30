package buildkite

type LogStreamerChunk struct {
	// The ID of the chunk as assigned by Buildkite
	ID string `json:"id,omitempty`

	// The sequence number of this chunk
	Order int `json:"order"`

	// The contents of the chunk
	Blob string `json:"blob"`

	// The size of the chunk
	Bytes int `json:"bytes"`

	// If this chunk has been uploaded
	Uploaded bool
}
