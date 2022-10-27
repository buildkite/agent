package clicommand

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli"
)

const envDescription = `Usage:
  buildkite-agent env [options]

Description:
   Prints out the environment of the current process as a JSON object, easily parsable by other programs. Used when
   executing hooks to discover changes that hooks make to the environment.

Example:
   $ buildkite-agent env

   Prints the environment passed into the process
`

var EnvCommand = cli.Command{
	Name:        "env",
	Usage:       "Prints out the environment of the current process as a JSON object",
	Description: envDescription,
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:   "pretty",
			Usage:  "Pretty print the JSON output",
			EnvVar: "BUILDKITE_AGENT_ENV_PRETTY",
		},
		cli.BoolFlag{
			Name:  "from-env-file",
			Usage: "Source environment from file described by $BUILDKITE_ENV_FILE",
		},
		cli.StringFlag{
			Name:  "print",
			Usage: "Print a single environment variable by `NAME` as raw text followed by a newline",
		},
	},
	Action: func(c *cli.Context) error {
		var envMap map[string]string
		var err error

		if c.Bool("from-env-file") {
			envMap, err = loadEnvFile(os.Getenv("BUILDKITE_ENV_FILE"))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading BUILDKITE_ENV_FILE: %v\n", err)
				os.Exit(1)
			}
		} else {
			env := os.Environ()
			envMap = make(map[string]string, len(env))

			for _, e := range env {
				k, v, _ := strings.Cut(e, "=")
				envMap[k] = v
			}
		}

		if name := c.String("print"); name != "" {
			fmt.Println(envMap[name])
			return nil
		}

		var envJSON []byte

		if c.Bool("pretty") {
			envJSON, err = json.MarshalIndent(envMap, "", "  ")
		} else {
			envJSON, err = json.Marshal(envMap)
		}

		// let's be polite to interactive shells etc.
		envJSON = append(envJSON, '\n')

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshalling JSON: %v\n", err)
			os.Exit(1)
		}

		if _, err := os.Stdout.Write(envJSON); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing JSON to stdout: %v\n", err)
			os.Exit(1)
		}

		return nil
	},
}

func loadEnvFile(path string) (map[string]string, error) {
	envMap := make(map[string]string)

	if path == "" {
		return nil, errors.New("no path specified")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(f)

	lineNo := 0
	for scanner.Scan() {
		lineNo++

		line := scanner.Text()
		if line == "" {
			continue
		}

		name, quotedValue, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("Unexpected format in %s:%d", path, lineNo)
		}

		value, err := strconv.Unquote(quotedValue)
		if err != nil {
			return nil, fmt.Errorf("unquoting value in %s:%d: %w", path, lineNo, err)
		}

		envMap[name] = value
	}

	return envMap, nil
}
