package edgeping

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"connectrpc.com/connect"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	agentv1 "github.com/buildkite/buildkite/agent-edge/gen/proto/buildkite/agent/v1"
	"github.com/buildkite/buildkite/agent-edge/gen/proto/buildkite/agent/v1/agentv1connect"
)

var ErrStreamUnavailable = errors.New("streaming ping unavailable")

type StreamPingSource struct {
	client        agentv1connect.AgentServiceClient
	logger        logger.Logger
	agentID       string
	stream        *connect.ServerStreamForClient[agentv1.PingResponse]
	streamCtx     context.Context
	streamCancel  context.CancelFunc
	retryAttempts int
	maxRetries    int
}

func edgeEndpointFromAgentEndpoint(agentEndpoint string) string {
	u, err := url.Parse(agentEndpoint)

	if err != nil {
		return agentEndpoint + "/connect"
	}

	u.Path = "/connect"
	return u.String()
}

func NewStreamPingSource(agentEndpoint string, agentID string, logger logger.Logger) *StreamPingSource {
	edgeEndpoint := edgeEndpointFromAgentEndpoint(agentEndpoint)

	return &StreamPingSource{
		client: agentv1connect.NewAgentServiceClient(
			http.DefaultClient,
			edgeEndpoint,
		),
		logger:     logger,
		agentID:    agentID,
		maxRetries: 5,
	}
}

func (s *StreamPingSource) connect(parentCtx context.Context) error {
	s.logger.Info("Connecting to StreamPings endpoint")

	streamCtx, cancel := context.WithCancel(parentCtx)

	type connectResult struct {
		stream *connect.ServerStreamForClient[agentv1.PingResponse]
		err    error
	}

	connectCh := make(chan connectResult, 1)
	go func() {
		stream, err := s.client.StreamPings(streamCtx, connect.NewRequest(&agentv1.StreamPingsRequest{
			AgentId: s.agentID,
		}))
		connectCh <- connectResult{stream: stream, err: err}
	}()

	select {
	case <-parentCtx.Done():
		cancel()
		return parentCtx.Err()
	case result := <-connectCh:
		if result.err != nil {
			cancel()
			return fmt.Errorf("failed to start stream: %w", result.err)
		}

		s.stream = result.stream
		s.streamCtx = streamCtx
		s.streamCancel = cancel
		s.retryAttempts = 0
		s.logger.Info("StreamPings connection established")
		return nil
	}
}

func (s *StreamPingSource) Next(ctx context.Context) (*PingEvent, error) {
	for {
		// Ensure we have a connection
		if s.stream == nil {
			if err := s.connect(ctx); err != nil {
				s.retryAttempts++
				if s.retryAttempts >= s.maxRetries {
					s.logger.Warn("Max retries reached for streaming, stream unavailable")
					return nil, ErrStreamUnavailable
				}

				backoff := time.Duration(1<<uint(s.retryAttempts-1)) * time.Second
				if backoff > 60*time.Second {
					backoff = 60 * time.Second
				}
				s.logger.Warn("Stream connection failed, retrying after backoff (attempt %d/%d): %v",
					s.retryAttempts, s.maxRetries, err)

				select {
				case <-time.After(backoff):
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}

		type receiveResult struct {
			msg *agentv1.PingResponse
			err error
		}

		receiveCh := make(chan receiveResult, 1)
		go func() {
			defer close(receiveCh)
			if s.stream.Receive() {
				if s.streamCtx != nil {
					select {
					case receiveCh <- receiveResult{msg: s.stream.Msg()}:
					case <-s.streamCtx.Done():
						return
					}
				} else {
					receiveCh <- receiveResult{msg: s.stream.Msg()}
				}
			} else {
				if s.streamCtx != nil {
					select {
					case receiveCh <- receiveResult{err: s.stream.Err()}:
					case <-s.streamCtx.Done():
						return
					}
				} else {
					receiveCh <- receiveResult{err: s.stream.Err()}
				}
			}
		}()

		select {
		case <-ctx.Done():
			s.logger.Info("Context cancelled, closing stream")
			if s.streamCancel != nil {
				s.streamCancel()
			}
			if s.stream != nil {
				s.stream.Close()
			}
			select {
			case <-receiveCh:
				s.logger.Info("Receive goroutine exited")
			case <-time.After(1 * time.Second):
				s.logger.Warn("Timed out waiting for receive goroutine; proceeding with shutdown")
			}
			return nil, ctx.Err()
		case result, ok := <-receiveCh:
			if !ok {
				return nil, context.Canceled
			}
			if result.err != nil {
				s.logger.Warn("Stream receive error, reconnecting: %v", result.err)
				if s.streamCancel != nil {
					s.streamCancel()
				}
				s.stream.Close()
				s.stream = nil
				continue
			}

			event := &PingEvent{}

			switch resp := result.msg.Response.(type) {
			case *agentv1.PingResponse_Idle:
				event.Action = "idle"
			case *agentv1.PingResponse_Pause:
				event.Action = "pause"
			case *agentv1.PingResponse_Disconnect:
				event.Action = "disconnect"
			case *agentv1.PingResponse_JobAssigned:
				if resp.JobAssigned.Job != nil {
					event.Job = &api.Job{ID: resp.JobAssigned.Job.Id}
				}
			}

			s.logger.Debug("Received ping from stream: action=%s, job=%v", event.Action, event.Job != nil)

			return event, nil
		}
	}
}

func (s *StreamPingSource) Close() error {
	if s.streamCancel != nil {
		s.streamCancel()
		s.streamCancel = nil
	}
	if s.stream != nil {
		s.stream.Close()
		s.stream = nil
	}
	return nil
}
