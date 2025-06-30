// Package self implements helpers for self-interaction.
package self

import (
	"context"
	"os"
)

// Overwritten by init.
var pathToSelf = "buildkite-agent"

func init() {
	p, err := os.Executable()
	if err != nil {
		return
	}
	pathToSelf = p
}

type ctxKey struct{}

// Path returns an absolute file path to buildkite-agent. If an absolute
// path cannot be found, it defaults to "buildkite-agent" on the assumption it
// is in $PATH. Self-executing with this path can still fail if someone is
// playing games (e.g. unlinking the binary after starting it).
func Path(ctx context.Context) string {
	if val := ctx.Value(ctxKey{}); val != nil {
		return val.(string)
	}
	return pathToSelf
}

// OverridePath changes the self-path used within a context. This is usually
// only used for testing purposes (creating a mock of buildkite-agent.)
func OverridePath(parent context.Context, path string) context.Context {
	return context.WithValue(parent, ctxKey{}, path)
}
