package clicommand

import (
	"context"
	"errors"
	"fmt"

	"github.com/buildkite/agent/v3/lock"
	"github.com/urfave/cli"
)

const lockGetHelpDescription = `Usage:

    buildkite-agent lock get [key]

Description:

Retrieves the value of a lock key. Any key not in use returns an empty
string.

Note that this subcommand is only available when an agent has been started
with the ′agent-api′ experiment enabled.

′lock get′ is generally only useful for inspecting lock state, as the value
can change concurrently. To acquire or release a lock, use ′lock acquire′ and
′lock release′.

Examples:

    $ buildkite-agent lock get llama
    Kuzco`

type LockGetConfig struct {
	GlobalConfig
	LockCommonConfig
}

var LockGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Gets a lock value from the agent leader",
	Description: lockGetHelpDescription,
	Flags:       lockCommonFlags(),
	Action:      lockGetAction,
}

func lockGetAction(c *cli.Context) error {
	if c.NArg() != 1 {
		fmt.Fprint(c.App.ErrWriter, lockGetHelpDescription) //nolint:errcheck // CLI help output
		return &SilentExitError{code: 1}
	}
	key := c.Args()[0]

	ctx, cfg, _, _, done := setupLoggerAndConfig[LockGetConfig](context.Background(), c)
	defer done()

	if cfg.LockScope != "machine" {
		return errors.New("only 'machine' scope for locks is supported in this version")
	}

	client, err := lock.NewClient(ctx, cfg.SocketsPath)
	if err != nil {
		return fmt.Errorf(lockClientErrMessage, err)
	}

	v, err := client.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("couldn't get lock state: %w", err)
	}

	fmt.Fprintln(c.App.Writer, v) //nolint:errcheck // CLI output; errors are non-actionable

	return nil
}
