package registrationtoken

import (
	"encoding/json"
	"fmt"

	"github.com/urfave/cli/v3"
)

type flagSchema struct {
	Names []string
	Kind  string
}

func describeFlag(flag cli.Flag) (flagSchema, error) {
	schema := flagSchema{Names: flag.Names()}
	switch flag.(type) {
	case *cli.StringFlag:
		schema.Kind = "string"
	case *cli.StringSliceFlag:
		schema.Kind = "strings"
	case *cli.BoolFlag:
		schema.Kind = "bool"
	case *cli.IntFlag:
		schema.Kind = "int"
	case *cli.DurationFlag:
		schema.Kind = "duration"
	default:
		return flagSchema{}, fmt.Errorf("unsupported start flag type %T", flag)
	}
	return schema, nil
}

func (s flagSchema) flag() (cli.Flag, error) {
	if len(s.Names) == 0 {
		return nil, fmt.Errorf("missing flag name")
	}
	name, aliases := s.Names[0], s.Names[1:]
	switch s.Kind {
	case "string":
		return &cli.StringFlag{Name: name, Aliases: aliases}, nil
	case "strings":
		return &cli.StringSliceFlag{Name: name, Aliases: aliases}, nil
	case "bool":
		return &cli.BoolFlag{Name: name, Aliases: aliases}, nil
	case "int":
		return &cli.IntFlag{Name: name, Aliases: aliases}, nil
	case "duration":
		return &cli.DurationFlag{Name: name, Aliases: aliases}, nil
	default:
		return nil, fmt.Errorf("unsupported start flag kind %q", s.Kind)
	}
}

func EncodeFlags(flags []cli.Flag) ([]byte, error) {
	schema := make([]flagSchema, 0, len(flags))
	for _, flag := range flags {
		entry, err := describeFlag(flag)
		if err != nil {
			return nil, err
		}
		schema = append(schema, entry)
	}
	return json.Marshal(schema)
}

func DecodeFlags(data []byte) ([]cli.Flag, error) {
	var schema []flagSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("decoding start flags: %w", err)
	}
	if len(schema) == 0 {
		return nil, fmt.Errorf("missing start flags")
	}
	flags := make([]cli.Flag, 0, len(schema))
	for _, entry := range schema {
		flag, err := entry.flag()
		if err != nil {
			return nil, err
		}
		flags = append(flags, flag)
	}
	return flags, nil
}
