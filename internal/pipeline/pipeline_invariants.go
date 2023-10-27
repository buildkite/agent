package pipeline

import (
	"fmt"
	"strings"
)

type PipelineInvariants struct {
	OrganizationSlug string
	PipelineSlug     string
	Repository       string
}

var _ SignedFielder = (*CommandStepWithPipelineInvariants)(nil)

type CommandStepWithPipelineInvariants struct {
	CommandStep
	PipelineInvariants
}

// SignedFields returns the default fields for signing.
func (c *CommandStepWithPipelineInvariants) SignedFields() (map[string]any, error) {
	return map[string]any{
		"command":             c.Command,
		"env":                 c.Env,
		"plugins":             c.Plugins,
		"matrix":              c.Matrix,
		"pipeline_invariants": c.PipelineInvariants,
	}, nil
}

// ValuesForFields returns the contents of fields to sign.
func (c *CommandStepWithPipelineInvariants) ValuesForFields(fields []string) (map[string]any, error) {
	// Make a set of required fields. As fields is processed, mark them off by
	// deleting them.
	required := map[string]struct{}{
		"command":             {},
		"env":                 {},
		"plugins":             {},
		"matrix":              {},
		"pipeline_invariants": {},
	}

	out := make(map[string]any, len(fields))
	for _, f := range fields {
		delete(required, f)

		switch f {
		case "command":
			out["command"] = c.Command

		case "env":
			out["env"] = c.Env

		case "plugins":
			out["plugins"] = c.Plugins

		case "matrix":
			out["matrix"] = c.Matrix

		case "pipeline_invariants":
			out["pipeline_invariants"] = c.PipelineInvariants

		default:
			// All env:: values come from outside the step.
			if strings.HasPrefix(f, EnvNamespacePrefix) {
				break
			}

			return nil, fmt.Errorf("unknown or unsupported field for signing %q", f)
		}
	}

	if len(required) > 0 {
		missing := make([]string, 0, len(required))
		for k := range required {
			missing = append(missing, k)
		}
		return nil, fmt.Errorf("one or more required fields are not present: %v", missing)
	}
	return out, nil
}
