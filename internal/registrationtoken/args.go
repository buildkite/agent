package registrationtoken

import (
	"context"
	"io"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"
)

type argumentFlag struct {
	cli.Flag
	takesValue bool
	record     func(string, string, bool)
}

func (f *argumentFlag) IsBoolFlag() bool { return !f.takesValue }

func (f *argumentFlag) Set(name, value string) error {
	if err := f.Flag.Set(name, value); err != nil {
		return err
	}
	f.record(name, value, f.takesValue)
	return nil
}

type Option struct {
	index  int
	prefix string
	Value  string
}

type Args struct {
	Start   bool
	Help    bool
	Options []Option
}

func (a Args) Token() (string, bool) {
	if len(a.Options) == 0 {
		return "", false
	}
	return a.Options[len(a.Options)-1].Value, true
}

func (a Args) Replace(args []string, replacement string) []string {
	out := slices.Clone(args)
	for _, option := range a.Options {
		out[option.index] = option.prefix + replacement
	}
	return out
}

func ParseArgs(args []string, flags []cli.Flag) Args {
	start := -1
	root := &cli.Command{
		Name: "buildkite-agent", Version: "internal", Writer: io.Discard, ErrWriter: io.Discard,
		Action: func(context.Context, *cli.Command) error { return nil },
		Commands: []*cli.Command{{
			Name: "start", SkipFlagParsing: true,
			Action: func(_ context.Context, c *cli.Command) error {
				start = len(args) - c.Args().Len() - 1
				return nil
			},
		}},
	}
	if err := root.Run(context.Background(), append([]string{"buildkite-agent"}, args...)); err != nil || start < 0 {
		return Args{}
	}
	parsed := Args{}
	cursor := start + 1
	record := func(name, value string, takesValue bool) {
		for cursor < len(args) {
			index := cursor
			cursor++
			arg := strings.TrimSpace(args[index])
			flagName, _, equal := strings.Cut(strings.TrimLeft(arg, "-"), "=")
			if !strings.HasPrefix(arg, "-") || flagName != name {
				continue
			}
			prefix := ""
			if takesValue {
				if equal {
					prefix = args[index][:strings.Index(args[index], "=")+1]
				} else {
					index = cursor
					cursor++
				}
			}
			if name == "token" {
				parsed.Options = append(parsed.Options, Option{index: index, prefix: prefix, Value: value})
			}
			return
		}
	}
	command := &cli.Command{
		Name: "start", HideVersion: true, Writer: io.Discard, ErrWriter: io.Discard,
		Action: func(context.Context, *cli.Command) error {
			parsed.Start = true
			return nil
		},
	}
	for _, definition := range flags {
		takesValue := definition.(interface{ TakesValue() bool }).TakesValue()
		schema, err := describeFlag(definition)
		if err != nil {
			return Args{}
		}
		flag, err := schema.flag()
		if err != nil {
			return Args{}
		}
		command.Flags = append(command.Flags, &argumentFlag{Flag: flag, takesValue: takesValue, record: record})
	}
	_ = command.Run(context.Background(), args[start:])
	parsed.Help = command.Bool("help")
	return parsed
}
