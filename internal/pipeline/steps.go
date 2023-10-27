package pipeline

import (
	"errors"
	"fmt"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

var errSigningRefusedUnknownStepType = errors.New("refusing to sign pipeline containing a step of unknown type, because the pipeline could be incorrectly parsed - please contact support")

// Steps contains multiple steps. It is useful for unmarshaling step sequences,
// since it has custom logic for determining the correct step type.
type Steps []Step

// UnmarshalOrdered unmarshals a slice ([]any) into a slice of steps.
func (s *Steps) UnmarshalOrdered(o any) error {
	if o == nil {
		if *s == nil {
			// `steps: null` is normalised to an empty slice.
			*s = Steps{}
		}
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

func (s Steps) interpolate(tf stringTransformer) error {
	return interpolateSlice(tf, s)
}

// unmarshalStep unmarshals into the right kind of Step.
func unmarshalStep(o any) (Step, error) {
	switch o := o.(type) {
	case string:
		step, err := NewScalarStep(o)
		if err != nil {
			return &UnknownStep{Contents: o}, nil
		}
		return step, nil

	case *ordered.MapSA:
		return stepFromMap(o)

	default:
		return nil, fmt.Errorf("unmarshaling step: unsupported type %T", o)
	}
}

func stepFromMap(o *ordered.MapSA) (Step, error) {
	sType, hasType := o.Get("type")

	var step Step
	var err error
	if hasType {
		sTypeStr, ok := sType.(string)
		if !ok {
			return nil, fmt.Errorf("unmarshaling step: step's `type` key was %T (value %v), want string", sType, sType)
		}

		step, err = stepByType(sTypeStr)
		if err != nil {
			return nil, fmt.Errorf("unmarshaling step: %w", err)
		}
	} else {
		step, err = stepByKeyInference(o)
		if err != nil {
			return nil, fmt.Errorf("unmarshaling step: %w", err)
		}
	}

	// Decode the step (into the right step type).
	if err := ordered.Unmarshal(o, step); err != nil {
		// Hmm, maybe we picked the wrong kind of step?
		return &UnknownStep{Contents: o}, nil
	}
	return step, nil
}

func stepByType(sType string) (Step, error) {
	switch sType {
	case "command", "script":
		return new(CommandStep), nil
	case "wait", "waiter":
		return &WaitStep{Contents: map[string]any{}}, nil
	case "block", "input", "manual":
		return &InputStep{Contents: map[string]any{}}, nil
	case "trigger":
		return new(TriggerStep), nil
	case "group": // as far as i know this doesn't happen, but it's here for completeness
		return new(GroupStep), nil
	default:
		return nil, fmt.Errorf("unknown step type %q", sType)
	}
}

func stepByKeyInference(o *ordered.MapSA) (Step, error) {
	switch {
	case o.Contains("command") || o.Contains("commands") || o.Contains("plugins"):
		// NB: Some "command" step are commandless containers that exist
		// just to run plugins!
		return new(CommandStep), nil

	case o.Contains("wait") || o.Contains("waiter"):
		return new(WaitStep), nil

	case o.Contains("block") || o.Contains("input") || o.Contains("manual"):
		return new(InputStep), nil

	case o.Contains("trigger"):
		return new(TriggerStep), nil

	case o.Contains("group"):
		return new(GroupStep), nil

	default:
		return new(UnknownStep), nil
	}
}

// sign adds signatures to each command step (and recursively to any command
// steps that are within group steps. The steps are mutated directly, so an
// error part-way through may leave some steps un-signed.
func (s Steps) sign(key jwk.Key, env map[string]string, pInv *PipelineInvariants) error {
	for _, step := range s {
		switch step := step.(type) {
		case *CommandStep:
			stepWithInvariants := &CommandStepWithPipelineInvariants{
				CommandStep:        *step,
				PipelineInvariants: *pInv,
			}

			sig, err := Sign(key, env, stepWithInvariants)
			if err != nil {
				return fmt.Errorf("signing step with command %q: %w", step.Command, err)
			}
			step.Signature = sig

		case *GroupStep:
			if err := step.Steps.sign(key, env, pInv); err != nil {
				return fmt.Errorf("signing group step: %w", err)
			}

		case *UnknownStep:
			// Presence of an unknown step means we're missing some semantic
			// information about the pipeline. We could be not signing something
			// that needs signing. Rather than deferring the problem (so that
			// signature verification fails when an agent runs jobs) we return
			// an error now.
			return errSigningRefusedUnknownStepType
		}
	}
	return nil
}
