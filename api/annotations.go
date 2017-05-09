package api

import "fmt"

// AnnotationsService handles communication with the annotation related methods of the
// Buildkite Agent API.
type AnnotationsService struct {
	client *Client
}

// Annotation represents a Buildkite Agent API Annotation
type Annotation struct {
	Body    string `json:"body"`
	Context string `json:"context,omitempty"`
	Style   string `json:"style,omitempty"`
}

// Annotates a build in the Buildkite UI
func (cs *AnnotationsService) Create(jobId string, annotation *Annotation) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/annotations", jobId)

	req, err := cs.client.NewRequest("POST", u, annotation)
	if err != nil {
		return nil, err
	}

	return cs.client.Do(req, nil)
}
