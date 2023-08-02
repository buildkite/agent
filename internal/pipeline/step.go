package pipeline

import (
	"github.com/buildkite/agent/v3/internal/ordered"
)

// Step models a step in the pipeline. It will be a pointer to one of:
// - CommandStep
// - WaitStep
// - InputStep
// - TriggerStep
// - GroupStep
type Step interface {
	stepTag() // allow only the step types below

	// unmarshalStep is responsible for choosing the right kind of step to
	// unmarshal, but it uses the map representation to figure this out.
	// So we can assume *ordered.MapSA input for unmarshaling.
	unmarshalMap(*ordered.MapSA) error

	selfInterpolater
}
