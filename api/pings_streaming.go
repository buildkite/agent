package api

import (
	"context"
	"fmt"
	"iter"
	"net/url"

	"connectrpc.com/connect"
	agentedgev1 "github.com/buildkite/agent/v3/api/proto/gen"
	"github.com/buildkite/agent/v3/api/proto/gen/agentedgev1connect"
)

// StreamPings opens a ConnectRPC channel for streaming pings. It returns an
// iterator over received messages and any error that occurs.
func (c *Client) StreamPings(ctx context.Context, agentID string, opts ...connect.ClientOption) (iter.Seq2[*agentedgev1.StreamPingsResponse, error], error) {
	// The streaming endpoint is the same as the main endpoint,
	// minus the `/v3/`.
	u, err := url.Parse(c.conf.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing endpoint: %w", err)
	}
	u.Path = "/"

	cl := agentedgev1connect.NewAgentEdgeServiceClient(
		c.client,
		u.String(),
		connect.WithClientOptions(opts...),
	)

	// For the record, this feels too much like burying optional parameters
	// in a context, which I think is bad - https://pkg.go.dev/context says:
	// "Use context Values only for request-scoped data that transits processes
	// and APIs, not for passing optional parameters to functions."
	ctx, callInfo := connect.NewClientContext(ctx)
	h := callInfo.RequestHeader()
	h.Set("User-Agent", c.conf.UserAgent)
	stream, err := cl.StreamPings(ctx, connect.NewRequest(&agentedgev1.StreamPingsRequest{
		AgentId: agentID,
	}))
	if err != nil {
		return nil, fmt.Errorf("from StreamPings: %w", err)
	}

	return func(yield func(*agentedgev1.StreamPingsResponse, error) bool) {
		defer stream.Close() //nolint:errcheck // Best-effort cleanup
		for stream.Receive() {
			err := stream.Err()
			if err != nil {
				err = fmt.Errorf("stream.Err within receive loop: %w", err)
			}
			if !yield(stream.Msg(), err) {
				return
			}
		}
		if err := stream.Err(); err != nil {
			yield(nil, fmt.Errorf("stream.Err following receive loop: %w", err))
		}
	}, nil
}
