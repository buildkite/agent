package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestSubcommandHelpHidesHiddenCommands(t *testing.T) {
	var output bytes.Buffer
	cli.HelpPrinter(&output, subcommandHelpTemplate, &cli.Command{
		Name: "parent",
		Commands: []*cli.Command{
			{Name: "visible-child", Usage: "Visible description"},
			{Name: "hidden-child", Usage: "Hidden description", Hidden: true},
		},
	})
	for _, want := range []string{"visible-child", "Visible description"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("help missing %q: %s", want, &output)
		}
	}
	for _, unwanted := range []string{"hidden-child", "Hidden description"} {
		if strings.Contains(output.String(), unwanted) {
			t.Errorf("help includes %q: %s", unwanted, &output)
		}
	}
}
