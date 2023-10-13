package pipeline

import (
	"encoding/json"
	"errors"
)

// See the comment in step_scalar.go.

// InputStep models a block or input step.
//
// Standard caveats apply - see the package comment.
type InputStep struct {
	Scalar   string         `yaml:"-"`
	Contents map[string]any `yaml:",inline"`
}

func (s *InputStep) MarshalJSON() ([]byte, error) {
	if s.Scalar != "" {
		return json.Marshal(s.Scalar)
	}

	if len(s.Contents) == 0 {
		return []byte{}, errors.New("empty input step")
	}

	return json.Marshal(s.Contents)
}

func (s InputStep) interpolate(tf stringTransformer) error {
	return interpolateMap(tf, s.Contents)
}

func (*InputStep) stepTag() {}
