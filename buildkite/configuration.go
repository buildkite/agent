package buildkite

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/buildkite/agent/buildkite/machine"
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

	if machine.IsWindows() {
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

// This file parsing code was copied from:
// https://github.com/joho/godotenv/blob/master/godotenv.go
//
// The project is released under an MIT License, which can be seen here:
// https://github.com/joho/godotenv/blob/master/LICENCE

func readFile(filename string) (envMap map[string]string, err error) {
	file, err := os.Open(filename)
	if err != nil {
		return
	}
	defer file.Close()

	envMap = make(map[string]string)

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	for _, fullLine := range lines {
		if !isIgnoredLine(fullLine) {
			key, value, err := parseLine(fullLine)

			if err == nil {
				envMap[key] = value
			}
		}
	}
	return
}

func parseLine(line string) (key string, value string, err error) {
	if len(line) == 0 {
		err = errors.New("zero length string")
		return
	}

	// ditch the comments (but keep quoted hashes)
	if strings.Contains(line, "#") {
		segmentsBetweenHashes := strings.Split(line, "#")
		quotesAreOpen := false
		segmentsToKeep := make([]string, 0)
		for _, segment := range segmentsBetweenHashes {
			if strings.Count(segment, "\"") == 1 || strings.Count(segment, "'") == 1 {
				if quotesAreOpen {
					quotesAreOpen = false
					segmentsToKeep = append(segmentsToKeep, segment)
				} else {
					quotesAreOpen = true
				}
			}

			if len(segmentsToKeep) == 0 || quotesAreOpen {
				segmentsToKeep = append(segmentsToKeep, segment)
			}
		}

		line = strings.Join(segmentsToKeep, "#")
	}

	// now split key from value
	splitString := strings.SplitN(line, "=", 2)

	if len(splitString) != 2 {
		// try yaml mode!
		splitString = strings.SplitN(line, ":", 2)
	}

	if len(splitString) != 2 {
		err = errors.New("Can't separate key from value")
		return
	}

	// Parse the key
	key = splitString[0]
	if strings.HasPrefix(key, "export") {
		key = strings.TrimPrefix(key, "export")
	}
	key = strings.Trim(key, " ")

	// Parse the value
	value = splitString[1]
	// trim
	value = strings.Trim(value, " ")

	// check if we've got quoted values
	if strings.Count(value, "\"") == 2 || strings.Count(value, "'") == 2 {
		// pull the quotes off the edges
		value = strings.Trim(value, "\"'")

		// expand quotes
		value = strings.Replace(value, "\\\"", "\"", -1)
		// expand newlines
		value = strings.Replace(value, "\\n", "\n", -1)
	}

	return
}

func isIgnoredLine(line string) bool {
	trimmedLine := strings.Trim(line, " \n\t")
	return len(trimmedLine) == 0 || strings.HasPrefix(trimmedLine, "#")
}
