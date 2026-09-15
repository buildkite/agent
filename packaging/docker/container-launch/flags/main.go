package main

import (
	"fmt"
	"os"
	"slices"

	"github.com/buildkite/agent/v4/clicommand"
	"github.com/buildkite/agent/v4/internal/registrationtoken"
	"github.com/urfave/cli/v3"
)

func main() {
	flags := append(slices.Clone(clicommand.AgentStartCommand.Flags), cli.HelpFlag)
	data, err := registrationtoken.EncodeFlags(flags)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}
