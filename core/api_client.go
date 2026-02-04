package core

import (
	"context"

	"github.com/buildkite/agent/v3/api"
)

// APIClient defines the subset of client methods needed by core.
type APIClient interface {
	AcquireJob(context.Context, string, ...api.Header) (*api.Job, *api.Response, error)
	Connect(context.Context) (*api.Response, error)
	Disconnect(context.Context) (*api.Response, error)
	FinishJob(context.Context, *api.Job, *bool) (*api.Response, error)
	Register(context.Context, *api.AgentRegisterRequest) (*api.AgentRegisterResponse, *api.Response, error)
	StartJob(context.Context, *api.Job) (*api.Response, error)
	UploadChunk(context.Context, string, *api.Chunk) (*api.Response, error)
}
