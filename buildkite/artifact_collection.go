package buildkite

type ArtifactCollection struct {
	// The ID of the Job that these artifacts belong to
	JobID string

	Artifacts []*Artifact

	// The API used for communication
	API API
}

// Sends all the artifacts at once to the Buildkite Agent API. This will allow
// the UI to show what artifacts will be uploaded. Their state starts out as
// "new"
func (a *ArtifactCollection) Create() error {
	return a.API.Post("jobs/"+a.JobID+"/artifacts", &a.Artifacts, a.Artifacts)
}
