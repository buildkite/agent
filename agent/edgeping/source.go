package edgeping

import (
	"context"

	"github.com/buildkite/agent/v3/api"
)

type PingEvent struct {
	Job    *api.Job
	Action string
}

type PingSource interface {
	Next(ctx context.Context) (*PingEvent, error)
	Close() error
}
