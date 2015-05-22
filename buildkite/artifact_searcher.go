package buildkite

import (
	"net/url"
)

type ArtifactSearcher struct {
	// The ID of the Build that these artifacts belong to
	BuildID string

	// The API used for communication
	API API

	// The artifacts currently in the collection
	Artifacts []*Artifact
}

func (a *ArtifactSearcher) Search(query string, scope string) error {
	queryString := "?query=" + url.QueryEscape(query) + "&scope=" + url.QueryEscape(scope) + "&state=finished"

	return a.API.Get("builds/"+a.BuildID+"/artifacts/search"+queryString, &a.Artifacts)
}
