package api

import (
	"context"
	"fmt"
	"iter"
	"net/url"
	"sync"
	"time"

	"connectrpc.com/connect"
	agentedgev1 "github.com/buildkite/agent/v4/api/proto/gen"
	"github.com/buildkite/agent/v4/api/proto/gen/agentedgev1connect"
)

// StreamPings opens a ConnectRPC channel for streaming pings, waiting for the
// first complete message within the API timeout. It returns an iterator over
// received messages with no whole-response timeout.
func (c *Client) StreamPings(ctx context.Context, agentID string, opts ...connect.ClientOption) (iter.Seq2[*agentedgev1.StreamPingsResponse, error], error) {
	// The streaming endpoint is the same as the main endpoint,
	// minus the `/v3/`.
	u, err := url.Parse(c.conf.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing endpoint: %w", err)
	}
	u.Path = "/"

	cl := agentedgev1connect.NewAgentEdgeServiceClient(
		c.streamingClient,
		u.String(),
		connect.WithGRPC(),
		connect.WithClientOptions(opts...),
	)

	// In order to set request headers, we need to tweak a value set in the
	// context. To me, this feels too much like burying optional parameters
	// in a context, which I think is bad - https://pkg.go.dev/context says:
	// "Use context Values only for request-scoped data that transits processes
	// and APIs, not for passing optional parameters to functions."
	ctx, callInfo := connect.NewClientContext(ctx)
	h := callInfo.RequestHeader()

	// Add any request headers specified by the server during register/ping
	for k, values := range c.requestHeadersSnapshot() {
		for _, v := range values {
			h.Add(k, v)
		}
	}

	// The Authorization header is added by the custom transport.
	// Other methods add User-Agent in newRequest.
	// Note that this does not set the entire header.
	// ConnectRPC takes our value here and adds its own component *before* our
	// own, which violates the convention of decreasing importance
	// (see RFC 7231 section 5.5.3).
	h.Set("User-Agent", c.conf.UserAgent)
	ctx, cancel := context.WithCancel(ctx)
	var setup sync.Once
	var setupErr error
	var timer *time.Timer
	if c.client.Timeout > 0 {
		timer = time.AfterFunc(c.client.Timeout, func() {
			setup.Do(func() {
				setupErr = connect.NewError(connect.CodeDeadlineExceeded, fmt.Errorf("opening ping stream: %w", context.DeadlineExceeded))
				cancel()
			})
		})
	}

	stream, err := cl.StreamPings(ctx, connect.NewRequest(&agentedgev1.StreamPingsRequest{
		AgentId: agentID,
	}))
	received := err == nil && stream.Receive()
	if err == nil && !received {
		err = stream.Err()
	}

	// Arbitrate completion against the timer before exposing an established
	// stream. Even an already-scheduled callback mustn't cancel it later.
	setup.Do(func() {})
	if timer != nil {
		timer.Stop()
	}

	if setupErr != nil {
		err = setupErr
	}

	if err != nil {
		cancel()
		if stream != nil {
			_ = stream.Close()
		}

		return nil, fmt.Errorf("from StreamPings: %w", err)
	}

	return func(yield func(*agentedgev1.StreamPingsResponse, error) bool) {
		defer stream.Close() //nolint:errcheck // Best-effort cleanup
		defer cancel()
		for ok := received; ok; ok = stream.Receive() {
			msg := stream.Msg()

			headersDidChange := false
			if headers := msg.GetRequestHeaders(); headers != nil {
				values := headers.GetValues()
				if values == nil {
					values = map[string]string{}
				}
				headersDidChange = c.setRequestHeaders(values)
			}

			// Always yield the action before restarting so that a message
			// containing both an action and new headers doesn't lose the action.
			if !yield(msg, nil) {
				return
			}

			if headersDidChange {
				// Restart the stream to pick up the new headers
				return
			}
		}

		if err := stream.Err(); err != nil {
			yield(nil, err)
		}
	}, nil
}
