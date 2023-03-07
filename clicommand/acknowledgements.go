package clicommand

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"

	"github.com/urfave/cli"
)

const acknowledgementsHelpDescription = `Usage:
  buildkite-agent acknowledgements

Description:
   Prints the licenses and notices of open source software incorporated into
   this software.

Example:

	$ buildkite-agent acknowledgements`

//go:embed ACKNOWLEDGEMENTS.md.gz
var acknowledgements []byte

type AcknowledgementsConfig struct{}

var AcknowledgementsCommand = cli.Command{
	Name:        "acknowledgements",
	Usage:       "Prints the licenses and notices of open source software incorporated into this software.",
	Description: acknowledgementsHelpDescription,
	Action: func(c *cli.Context) error {
		r, err := gzip.NewReader(bytes.NewReader(acknowledgements))
		if err != nil {
			return fmt.Errorf("Couldn't create a gzip reader: %w", err)
		}
		if _, err := io.Copy(c.App.Writer, r); err != nil {
			return fmt.Errorf("Couldn't copy acknowledgments to output: %w", err)
		}
		return nil
	},
}
