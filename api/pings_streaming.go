package api

import (
	"context"
	"iter"

	"connectrpc.com/connect"
	agentedgev1 "github.com/buildkite/agent/v3/api/proto/gen"
	"github.com/buildkite/agent/v3/api/proto/gen/agentedgev1connect"
)

// StreamPings opens a ConnectRPC channel for streaming pings. It returns an
// iterator over received messages and any error that occurs.
func (c *Client) StreamPings(ctx context.Context, opts ...connect.ClientOption) (iter.Seq2[*agentedgev1.StreamPingsResponse, error], error) {
	cl := agentedgev1connect.NewAgentEdgeServiceClient(
		c.client,                           // ! TODO: consider debug logging/tracing aspect
		c.conf.Endpoint,                    // ! TODO: check endpoint configuration
		connect.WithClientOptions(opts...), // ! TODO: consider options, defaults, etc
	)
	stream, err := cl.StreamPings(ctx, connect.NewRequest(&agentedgev1.StreamPingsRequest{
		AgentId: c.conf.Token, // ! TODO: check this
	}))
	if err != nil {
		return nil, err
	}

	return func(yield func(*agentedgev1.StreamPingsResponse, error) bool) {
		defer stream.Close() //nolint:errcheck // Best-effort cleanup
		for stream.Receive() {
			if !yield(stream.Msg(), stream.Err()) {
				return
			}
		}
		if err := stream.Err(); err != nil {
			yield(nil, err)
		}
	}, nil
}
