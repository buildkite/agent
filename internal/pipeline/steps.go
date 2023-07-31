package pipeline

import (
	"fmt"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/interpolate"
)

// Steps contains multiple steps. It is useful for unmarshaling step sequences,
// since it has custom logic for determining the correct step type.
type Steps []Step

// unmarshalAny unmarshals a slice ([]any) into a slice of steps.
func (s *Steps) unmarshalAny(o any) error {
	if o == nil && *s == nil {
		*s = Steps{}
		return nil
	}
	sl, ok := o.([]any)
	if !ok {
		return fmt.Errorf("unmarshaling steps: got %T, want a slice ([]any)", sl)
	}
	// Preallocate slice if not already allocated
	if *s == nil {
		*s = make(Steps, 0, len(sl))
	}
	for _, st := range sl {
		step, err := unmarshalStep(st)
		if err != nil {
			return err
		}
		*s = append(*s, step)
	}
	return nil
}

func (s Steps) interpolate(env interpolate.Env) error {
	return interpolateSlice(env, s)
}

// unmarshalStep unmarshals into the right kind of Step.
func unmarshalStep(o any) (Step, error) {
	switch o := o.(type) {
	case string:
		step, err := NewScalarStep(o)
		if err != nil {
			return nil, fmt.Errorf("unmarshaling step: %w", err)
		}
		return step, nil

	case *ordered.MapSA:
		var step Step
		switch {
		case o.Contains("command") || o.Contains("commands") || o.Contains("plugins"):
			// NB: Some "command" step are commandless containers that exist
			// just to run plugins!
			step = new(CommandStep)

		case o.Contains("wait") || o.Contains("waiter"):
			step = make(WaitStep)

		case o.Contains("block") || o.Contains("input") || o.Contains("manual"):
			step = make(InputStep)

		case o.Contains("trigger"):
			step = make(TriggerStep)

		case o.Contains("group"):
			step = new(GroupStep)

		default:
			return nil, fmt.Errorf("unknown step type")
		}

		// Decode the step (into the right step type).
		return step, step.unmarshalMap(o)

	default:
		return nil, fmt.Errorf("unmarshaling step: unsupported type %T", o)
	}
}

// sign adds signatures to each command step (and recursively to any command
// steps that are within group steps. The steps are mutated directly, so an
// error part-way through may leave some steps un-signed.
func (s Steps) sign(signer Signer) error {
	for _, step := range s {
		switch step := step.(type) {
		case *CommandStep:
			sig, err := Sign(step, signer)
			if err != nil {
				return fmt.Errorf("signing step with command %q: %w", step.Command, err)
			}
			step.Signature = sig

		case *GroupStep:
			if err := step.Steps.sign(signer); err != nil {
				return fmt.Errorf("signing group step: %w", err)
			}
		}
	}
	return nil
}
