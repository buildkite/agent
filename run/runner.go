package run

import (
	"fmt"

	"github.com/buildkite/agent/logger"
)

type Runner struct {
	Pipeline interface{}
	Step     string

	BuildPath       string
	PluginPath      string
	BootstrapScript string
}

func (r *Runner) Run() error {
	steps, err := parseSteps(r.Pipeline)
	if err != nil {
		return err
	}
	for _, step := range steps {
		if c, ok := step.(Command); ok {
			logger.Debug("Executing Command Step: %q", c.String())

			plugins, err := c.Plugins()
			if err != nil {
				return err
			}

			executor := &CommandExecutor{
				Command:         c.Command(),
				Plugins:         plugins,
				BuildPath:       r.BuildPath,
				PluginPath:      r.PluginPath,
				BootstrapScript: r.BootstrapScript,
			}

			if err := executor.Execute(); err != nil {
				return err
			}
		}
	}
	return nil
}

type Step interface {
	String() string
}

type Command map[string]interface{}

func (s Command) Command() []string {
	if command, ok := s[`command`].(string); ok {
		return []string{command}
	}
	// TODO: Handle multi-lines
	return nil
}

func (s Command) String() string {
	if label, ok := s[`label`].(string); ok {
		return label
	}
	if name, ok := s[`name`].(string); ok {
		return name
	}
	return "step"
}

func (s Command) Plugins() ([]Plugin, error) {
	var plugins []Plugin

	if _, exists := s[`plugins`]; !exists {
		return plugins, nil
	}

	switch p := s[`plugins`].(type) {
	case map[string]interface{}:
		for k, v := range p {
			params, ok := v.(map[string]interface{})
			if !ok {
				logger.Warn("Unknown type of plugin param %T: %v", v, v)
			}
			plugins = append(plugins, Plugin{k, params})
		}
	default:
		return nil, fmt.Errorf("Unhandled plugin type %T", s[`plugins`])
	}

	return plugins, nil
}

type Wait string

func (s Wait) String() string {
	return "wait"
}

func parseStep(s interface{}) (Step, error) {
	switch step := s.(type) {
	case map[string]interface{}:
		return Command(step), nil
	case string:
		return Wait(step), nil
	default:
		panic(fmt.Sprintf("Unhandled step type %T", s))
	}
}

func parseSteps(p interface{}) ([]Step, error) {
	var steps []Step

	switch pipeline := p.(type) {
	// handle top-level dict with env and steps
	case map[string]interface{}:
		for k, v := range pipeline {
			if k == `steps` {
				for _, vs := range v.([]interface{}) {
					step, err := parseStep(vs)
					if err != nil {
						return nil, err
					}
					steps = append(steps, step)
				}
			}
		}
	// handle top level list of steps
	case []interface{}:
		for _, v := range pipeline {
			step, err := parseStep(v)
			if err != nil {
				return nil, err
			}
			steps = append(steps, step)
		}
	default:
		return nil, fmt.Errorf("Unhandled type %T in pipeline", p)
	}

	return steps, nil
}
