// Package cliconfig provides a configuration file loader.
//
// It is intended for internal use by buildkite-agent only.
package cliconfig

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/agent/v3/logger"
	"github.com/oleiade/reflections"
	"github.com/urfave/cli"
)

type Loader struct {
	// The context that is passed when using a urfave/cli action
	CLI *cli.Context

	// The struct that the config values will be loaded into
	Config any

	// The logger used
	Logger logger.Logger

	// A slice of paths to files that should be used as config files
	DefaultConfigFilePaths []string

	// The file that was used when loading this configuration
	File *File
}

// Matches "arg:index" (specific non-flag arg) or "arg:*" (all non-flag args).
var argCLINameRE = regexp.MustCompile(`arg:(\d+|\*)`)

// Loads the config from the CLI and config files that are present and returns
// any warnings or errors
func (l *Loader) Load() (warnings []string, err error) {
	// Try and find a config file, either passed in the command line using
	// --config, or in one of the default configuration file paths.
	if l.CLI.String("config") != "" {
		file := File{Path: l.CLI.String("config")}

		// Because this file was passed in manually, we should throw an error
		// if it doesn't exist.
		if file.Exists() {
			l.File = &file
		} else {
			absolutePath, _ := file.AbsolutePath()
			return warnings, fmt.Errorf("a configuration file could not be found at: %q", absolutePath)
		}
	} else if len(l.DefaultConfigFilePaths) > 0 {
		for _, path := range l.DefaultConfigFilePaths {
			file := File{Path: path}

			// If the config file exists, save it to the loader and
			// don't bother checking the others.
			if file.Exists() {
				l.File = &file
				break
			}
		}
	}

	// If a file was found, then we should load it
	if l.File != nil {
		// Attempt to load the config file we've found
		if err := l.File.Load(); err != nil {
			return warnings, fmt.Errorf("loading config file: %w", err)
		}
	}

	// Now it's onto actually setting the fields. We start by getting all
	// the fields from the configuration interface
	var fields []string
	fields, _ = reflections.FieldsDeep(l.Config)

	// Loop through each of the fields, and look for tags and handle them
	// appropriately
	for _, fieldName := range fields {
		// Start by loading the value from the CLI context if the tag
		// exists
		cliName, _ := reflections.GetFieldTag(l.Config, fieldName, "cli")
		if cliName != "" {
			// Load the value from the CLI Context
			err := l.setFieldValueFromCLI(fieldName, cliName)
			if err != nil {
				return warnings, fmt.Errorf("setting config field %s: %w", fieldName, err)
			}
		}

		// Are there any normalizations we need to make?
		normalization, _ := reflections.GetFieldTag(l.Config, fieldName, "normalize")
		if normalization != "" {
			// Apply the normalization
			err := l.normalizeField(fieldName, normalization)
			if err != nil {
				return warnings, fmt.Errorf("normalizing config field %s: %w", fieldName, err)
			}
		}

		// Check for field rename deprecations
		renamedToFieldName, _ := reflections.GetFieldTag(l.Config, fieldName, "deprecated-and-renamed-to")
		if renamedToFieldName != "" {
			// If the deprecated field's value isn't empty, then we
			// log a message, and set the proper config for them.
			if !l.fieldValueIsEmpty(fieldName) {
				renamedFieldCliName, _ := reflections.GetFieldTag(l.Config, renamedToFieldName, "cli")
				if renamedFieldCliName != "" {
					warnings = append(warnings,
						fmt.Sprintf("The config option `%s` has been renamed to `%s`. Please update your configuration.", cliName, renamedFieldCliName))
				}

				value, _ := reflections.GetField(l.Config, fieldName)

				// Error if they specify the deprecated version and the new version
				if !l.fieldValueIsEmpty(renamedToFieldName) {
					renamedFieldValue, _ := reflections.GetField(l.Config, renamedToFieldName)
					return warnings, fmt.Errorf("couldn't set config option `%s=%v`, `%s=%v` has already been set", cliName, value, renamedFieldCliName, renamedFieldValue)
				}

				// Set the proper config based on the deprecated value
				if value != nil {
					err := reflections.SetField(l.Config, renamedToFieldName, value)
					if err != nil {
						return warnings, fmt.Errorf("setting field %q to value %q: %w", renamedToFieldName, value, err)
					}
				}
			}
		}

		// Check for field deprecation
		deprecationError, _ := reflections.GetFieldTag(l.Config, fieldName, "deprecated")
		if deprecationError != "" {
			// If the deprecated field's value isn't empty, then we
			// return the deprecation error message.
			if !l.fieldValueIsEmpty(fieldName) {
				warnings = append(warnings,
					fmt.Sprintf("The config option `%s` has been deprecated: %s", cliName, deprecationError))
			}
		}

		// Perform validations
		validationRules, _ := reflections.GetFieldTag(l.Config, fieldName, "validate")
		if validationRules != "" {
			// Determine the label for the field
			label, _ := reflections.GetFieldTag(l.Config, fieldName, "label")
			if label == "" {
				// Use the cli name if it exists, but if it
				// doesn't, just default to the structs field
				// name. Not great, but works!
				if cliName != "" {
					label = cliName
				} else {
					label = fieldName
				}
			}

			// Validate the fieid, and if it fails, return its
			// error.
			err := l.validateField(fieldName, label, validationRules)
			if err != nil {
				return warnings, err
			}
		}
	}

	return warnings, nil
}

func (l Loader) setFieldValueFromCLI(fieldName, cliName string) error {
	// Get the kind of field we need to set
	fieldKind, err := reflections.GetFieldKind(l.Config, fieldName)
	if err != nil {
		return fmt.Errorf("getting the kind of struct field %q: %w", fieldName, err)
	}
	fieldType, err := reflections.GetFieldType(l.Config, fieldName)
	if err != nil {
		return fmt.Errorf("getting the type of struct field %q: %w", fieldName, err)
	}

	var value any

	// See the if the cli option is using the arg format (arg:1)
	argMatch := argCLINameRE.FindStringSubmatch(cliName)
	if len(argMatch) > 0 {
		argNum := argMatch[1]

		if argNum == "*" {
			// All args.
			value = l.CLI.Args()
		} else {
			// It's an index.
			// Convert the arg position to an integer
			argIndex, err := strconv.Atoi(argNum)
			if err != nil {
				return fmt.Errorf("converting string to int: %w", err)
			}

			// Only set the value if the args are long enough for
			// the position to exist.
			if len(l.CLI.Args()) > argIndex {
				value = l.CLI.Args()[argIndex]
			}
		}

		// Otherwise see if we can pull it from an environment variable
		// (and fail gracefully if we can't)
		if value == nil {
			envName, err := reflections.GetFieldTag(l.Config, fieldName, "env")
			if err == nil {
				if envValue, envSet := os.LookupEnv(envName); envSet {
					value = envValue
				}
			}
		}

	} else {
		// If the cli name didn't have the special format, then we need to
		// either load from the context's flags, or from a config file.

		// We start by defaulting the value to what ever was provided
		// by the configuration file
		if l.File != nil {
			if configFileValue, ok := l.File.Config[cliName]; ok {
				// Convert the config file value to its correct type
				switch fieldKind {
				case reflect.String:
					value = configFileValue
				case reflect.Slice:
					value = strings.Split(configFileValue, ",")
				case reflect.Bool:
					value, _ = strconv.ParseBool(configFileValue)
				case reflect.Int:
					value, _ = strconv.Atoi(configFileValue)
				case reflect.Int64:
					switch fieldType {
					case "int64":
						value, _ = strconv.ParseInt(configFileValue, 10, 64)
					case "time.Duration":
						value, _ = time.ParseDuration(configFileValue)
					default:
						return fmt.Errorf("unsupported field type %s for kind int64", fieldType)
					}
				default:
					return fmt.Errorf("unable to convert string to type %s", fieldKind)
				}
			}
		}

		// If a value hasn't been found in a config file, but there
		// _is_ one provided by the CLI context, then use that.
		if value == nil || l.cliValueIsSet(cliName) {
			switch fieldKind {
			case reflect.String:
				value = l.CLI.String(cliName)
			case reflect.Slice:
				value = l.CLI.StringSlice(cliName)
			case reflect.Bool:
				value = l.CLI.Bool(cliName)
			case reflect.Int:
				value = l.CLI.Int(cliName)
			case reflect.Int64:
				switch fieldType {
				case "int64":
					value = l.CLI.Int64(cliName)
				case "time.Duration":
					value = l.CLI.Duration(cliName)
				default:
					return fmt.Errorf("unsupported field type %s for kind int64", fieldType)
				}
			default:
				return fmt.Errorf("unable to handle type: %s", fieldKind)
			}
		}
	}

	// Set the value to the cfg
	if value != nil {
		err = reflections.SetField(l.Config, fieldName, value)
		if err != nil {
			return fmt.Errorf("setting value field %q to %q: %w", fieldName, value, err)
		}
	}

	return nil
}

func (l Loader) Errorf(format string, v ...any) error {
	suffix := fmt.Sprintf(" See: `%s %s --help`", l.CLI.App.Name, l.CLI.Command.Name)

	return fmt.Errorf(format+suffix, v...)
}

func (l Loader) cliValueIsSet(cliName string) bool {
	if l.CLI.IsSet(cliName) {
		return true
	} else {
		// cli.Context#IsSet only checks to see if the command was set via the cli, not
		// via the environment. So here we do some hacks to find out the name of the
		// EnvVar, and return true if it was set.
		for _, flag := range l.CLI.Command.Flags {
			name, _ := reflections.GetField(flag, "Name")
			envVar, _ := reflections.GetField(flag, "EnvVar")
			if name == cliName && envVar != "" {
				// Make sure envVar is a string
				if envVarStr, ok := envVar.(string); ok {
					envVarStr = strings.TrimSpace(string(envVarStr))

					return os.Getenv(envVarStr) != ""
				}
			}
		}
	}

	return false
}

func (l Loader) fieldValueIsEmpty(fieldName string) bool {
	// We need to use the field kind to determine the type of empty test.
	value, _ := reflections.GetField(l.Config, fieldName)
	fieldKind, _ := reflections.GetFieldKind(l.Config, fieldName)

	switch fieldKind {
	case reflect.String:
		return value == ""
	case reflect.Slice:
		v := reflect.ValueOf(value)
		return v.Len() == 0
	case reflect.Bool:
		return value == false
	case reflect.Int:
		return value == 0
	default:
		panic(fmt.Sprintf("Can't determine empty-ness for field type %s", fieldKind))
	}
}

func (l Loader) validateField(fieldName, label, validationRules string) error {
	// Split up the validation rules
	rules := strings.SplitSeq(validationRules, ",")

	// Loop through each rule, and perform it
	for rule := range rules {
		switch rule {
		case "required":
			if l.fieldValueIsEmpty(fieldName) {
				return l.Errorf("Missing %s.", label)
			}

		case "file-exists":
			value, _ := reflections.GetField(l.Config, fieldName)

			// Make sure the value is converted to a string
			if valueAsString, ok := value.(string); ok {
				// Return an error if the path doesn't exist
				if _, err := os.Stat(valueAsString); err != nil {
					return fmt.Errorf("couldn't find %s located at %s: %w", label, value, err)
				}
			}

		default:
			return fmt.Errorf("unknown config validation rule %q", rule)
		}
	}

	return nil
}

func (l Loader) normalizeField(fieldName, normalization string) error {
	if normalization == "filepath" {
		value, _ := reflections.GetField(l.Config, fieldName)
		fieldKind, _ := reflections.GetFieldKind(l.Config, fieldName)

		// Make sure we're normalizing a string field
		if fieldKind != reflect.String {
			return fmt.Errorf("filepath normalization only works on string fields")
		}

		// Normalize the field to be a filepath
		if valueAsString, ok := value.(string); ok {
			normalizedPath, err := osutil.NormalizeFilePath(valueAsString)
			if err != nil {
				return err
			}

			if err := reflections.SetField(l.Config, fieldName, normalizedPath); err != nil {
				return err
			}
		}
	} else if normalization == "commandpath" {
		value, _ := reflections.GetField(l.Config, fieldName)
		fieldKind, _ := reflections.GetFieldKind(l.Config, fieldName)

		// Make sure we're normalizing a string field
		if fieldKind != reflect.String {
			return fmt.Errorf("commandpath normalization only works on string fields")
		}

		// Normalize the field to be a command
		if valueAsString, ok := value.(string); ok {
			normalizedCommandPath, err := osutil.NormalizeCommand(valueAsString)
			if err != nil {
				return err
			}

			if err := reflections.SetField(l.Config, fieldName, normalizedCommandPath); err != nil {
				return err
			}
		}
	} else if normalization == "list" {
		value, _ := reflections.GetField(l.Config, fieldName)
		fieldKind, _ := reflections.GetFieldKind(l.Config, fieldName)

		// Make sure we're normalizing a string field
		if fieldKind != reflect.Slice {
			return fmt.Errorf("list normalization only works on slice fields")
		}

		// Normalize the field to be a string
		if valueAsSlice, ok := value.([]string); ok {
			normalizedSlice := []string{}

			for _, value := range valueAsSlice {
				// Split values with commas into fields
				for normalized := range strings.SplitSeq(value, ",") {
					if normalized == "" {
						continue
					}

					normalized = strings.TrimSpace(normalized)

					normalizedSlice = append(normalizedSlice, normalized)
				}
			}

			if err := reflections.SetField(l.Config, fieldName, normalizedSlice); err != nil {
				return err
			}
		}

	} else {
		return fmt.Errorf("unknown normalization %q", normalization)
	}

	return nil
}
