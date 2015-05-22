package buildkite

import (
	"net/url"
)

type ArtifactCollection struct {
	// The ID of the Job that these artifacts belong to
	JobID string

	// The API used for communication
	API API

	// The artifacts currently in the collection
	Artifacts []*Artifact
}

func (a *ArtifactCollection) Create() error {
	return a.API.Post("jobs/"+a.JobID+"/artifacts", &a.Artifacts, a.Artifacts)
}

func (a *ArtifactCollection) Search(query string, step string) error {
	queryString := "?query=" + url.QueryEscape(query) + "&step=" + url.QueryEscape(step) + "&state=finished"

	return a.API.Get("jobs/"+a.JobID+"/artifacts/search"+queryString, &a.Artifacts)
}
