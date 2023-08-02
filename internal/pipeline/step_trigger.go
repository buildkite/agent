package pipeline

import (
	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/interpolate"
)

// TriggerStep models a trigger step.
//
// Standard caveats apply - see the package comment.
type TriggerStep map[string]any

func (s TriggerStep) interpolate(env interpolate.Env) error {
	return interpolateMap(env, s)
}

func (s TriggerStep) unmarshalMap(m *ordered.MapSA) error {
	return m.Range(func(k string, v any) error {
		s[k] = v
		return nil
	})
}

func (TriggerStep) stepTag() {}
