package pipeline

import (
	"encoding/json"
)

// See the comment in step_scalar.go.

// WaitStep models a wait step.
//
// Standard caveats apply - see the package comment.
type WaitStep struct {
	Scalar   string         `yaml:"-"`
	Contents map[string]any `yaml:",inline"`
}

// MarshalJSON marshals a wait step as "wait" if w is empty, or as the step's scalar if it's set.
// If scalar is empty, it marshals as the remaining fields
func (s *WaitStep) MarshalJSON() ([]byte, error) {
	if s.Scalar != "" {
		return json.Marshal(s.Scalar)
	}

	if len(s.Contents) == 0 {
		return json.Marshal("wait")
	}

	return json.Marshal(s.Contents)
}

func (s *WaitStep) interpolate(tf stringTransformer) error {
	return interpolateMap(tf, s.Contents)
}

func (*WaitStep) stepTag() {}
