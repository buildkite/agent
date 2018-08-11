package agent

import (
	"fmt"
	"reflect"
	"strings"
)

type StepSigner struct {
	SigningKey string
}

func (s StepSigner) SignPipeline(pipeline interface{}) (interface{}, error) {
	original := reflect.ValueOf(pipeline)

	// only process pipelines that are either a single complex step (not "wait") or a collection of steps
	if original.Kind() != reflect.Map {
		return pipeline, nil
	}

	copy := reflect.MakeMap(original.Type())

	// Copy values to new map
	// TODO handle pipelines of single commands (e.g. `command: foo`)
	for _, mk := range original.MapKeys() {
		keyName := mk.String()
		item := original.MapIndex(mk)

		// references many steps
		if strings.EqualFold(keyName, "steps") {
			unwrapped := item.Elem()
			if unwrapped.Kind() == reflect.Slice {
				var newSteps []interface{}
				for i := 0; i < unwrapped.Len(); i += 1 {
					stepItem := unwrapped.Index(i)
					if stepItem.Elem().Kind() != reflect.String {
						newSteps = append(newSteps, s.signStep(stepItem))
					} else {
						newSteps = append(newSteps, stepItem.Interface())
					}
				}
				item = reflect.ValueOf(newSteps)
			}
		}
		copy.SetMapIndex(mk, item)
	}

	return copy.Interface(), nil
}

func (s StepSigner) signStep(step reflect.Value) (interface{}) {
	original := step.Elem()

	// Check to make sure the interface isn't nil
	if !original.IsValid() {
		return nil
	}

	// Create a new object
	copy := make(map[string]interface{})
	for _, key := range original.MapKeys() {
		copy[key.String()] = original.MapIndex(key).Interface()
	}

	rawCommand, hasCommand := copy["command"]
	if !hasCommand {
		// treat commands as an alias of command
		var hasCommands bool
		rawCommand, hasCommands = copy["commands"]
		if !hasCommands {
			// no commands to sign
			return copy
		}
	}

	commandSignature := s.signCommand(rawCommand)

	env := make(map[string]interface{})
	existingEnv, hasEnv := copy["env"]
	if hasEnv {
		reflectedEnv := reflect.ValueOf(existingEnv)
		for _, key := range reflectedEnv.MapKeys() {
			env[key.String()] = reflectedEnv.MapIndex(key).Interface()
		}
	}

	env["BUILDKITE_STEP_SIGNATURE"] = commandSignature
	copy["env"] = env

	return copy
}

func (s StepSigner) signCommand(command interface{}) string {
	value := reflect.ValueOf(command)

	// expand into simple list of commands
	var commandStrings []string
	if value.Kind() == reflect.Slice {
		for i := 0; i < value.Len(); i += 1 {
			commandStrings = append(commandStrings, value.Index(i).Elem().String())
		}
	} else if value.Kind() == reflect.String {
		commandStrings = append(commandStrings, value.String())
	} else {
		// BOOM
	}

	// TODO: HMAC this
	return fmt.Sprintf("%s%s", s.SigningKey, strings.Join(commandStrings, ""))
}
