package config

import (
	"errors"
	"fmt"
	"github.com/codegangsta/cli"
	"github.com/oleiade/reflections"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

func LoadConfiguration(obj interface{}, c *cli.Context) error {
	configFileMap := map[string]string{}

	var pathToConfigFile string

	// If a config file was passed, load it into a map
	if c.String("config") != "" {
		pathToConfigFile = os.ExpandEnv(c.String("config"))
	} else {
		// If no config was passed, look at the default locations to
		// try and find one that exists
		pathToConfigFile = findDefaultConfiguration()
	}

	// If we need to load from a config file
	if pathToConfigFile != "" {
		loadedConfigFileMap, err := readFile(pathToConfigFile)
		if err != nil {
			return errors.New(fmt.Sprintf("Failed to load config file: %s", err))
		}

		configFileMap = loadedConfigFileMap

		// Store the loaded file in the configuration object
		_ = reflections.SetField(obj, "File", pathToConfigFile)
	}

	// Get all the fields from the configuration interface
	var fields []string
	fields, _ = reflections.Fields(obj)

	for _, name := range fields {
		// Check if the value needs to be loaded from the cli.Context
		cliName, err := reflections.GetFieldTag(obj, name, "cli")
		if err != nil || cliName == "" {
			continue
		}

		// Get the kind of field we need to set
		fieldKind, err := reflections.GetFieldKind(obj, name)
		if err != nil {
			return errors.New(fmt.Sprintf("Failed to get the type of struct field \"%s\"", name))
		}

		var value interface{}

		// Start by defaulting the value to what ever was provided by
		// the configuration file
		if configFileValue, ok := configFileMap[cliName]; ok {
			// Convert the config file value to it's correct type
			if fieldKind == reflect.String {
				value = configFileValue
			} else if fieldKind == reflect.Slice {
				value = strings.Split(configFileValue, ",")
			} else if fieldKind == reflect.Bool {
				value, _ = strconv.ParseBool(configFileValue)
			} else {
				return errors.New(fmt.Sprintf("Unable to convert string to type %s", fieldKind))
			}
		}

		// If a value hasn't been defined, or if it's been overridden
		// but the ENV or the CLI, use that. We need to check for nil
		// because the cli also returns the default value.
		if value == nil || c.IsSet(cliName) || isSetByEnv(c, cliName) {
			if fieldKind == reflect.String {
				value = c.String(cliName)
			} else if fieldKind == reflect.Slice {
				value = c.StringSlice(cliName)
			} else if fieldKind == reflect.Bool {
				value = c.Bool(cliName)
			} else {
				return errors.New(fmt.Sprintf("Unable to handle type: %s", fieldKind))
			}
		}

		// Set the value if we've found data for it
		if value != nil {
			err = reflections.SetField(obj, name, value)
			if err != nil {
				return errors.New(fmt.Sprintf("Failed to set a value to struct field \"%s\"", name))
			}
		}
	}

	return nil
}

// cli.Context#IsSet only checks to see if the command was set via the cli, not
// via the environment. So here we do some hacks to find out the name of the
// EnvVar, and return true if it was set.
func isSetByEnv(c *cli.Context, cliName string) bool {
	for _, flag := range c.Command.Flags {
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

	return false
}

func findDefaultConfiguration() string {
	var paths []string

	if MachineIsWindows() {
		paths = []string{
			"$USERPROFILE\\AppData\\Local\\BuildkiteAgent\\buildkite-agent.cfg",
		}
	} else {
		paths = []string{
			"$HOME/.buildkite-agent/buildkite-agent.cfg",
			"/usr/local/etc/buildkite-agent/buildkite-agent.cfg",
			"/etc/buildkite-agent/buildkite-agent.cfg",
		}
	}

	// Also check to see if there's a buildkite-agent.cfg in the folder
	// that the binary is running in.
	pathToBinary, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err == nil {
		pathToRelativeConfig := filepath.Join(pathToBinary, "buildkite-agent.cfg")
		paths = append([]string{pathToRelativeConfig}, paths...)
	}

	// Return the first configration file that exists
	for _, path := range paths {
		expandedPath := os.ExpandEnv(path)

		if _, err := os.Stat(expandedPath); err == nil {
			return expandedPath
		}
	}

	return ""
}
