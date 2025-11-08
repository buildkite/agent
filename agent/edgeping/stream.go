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

func (s *StreamPingSource) connect(ctx context.Context) error {
	s.logger.Info("Connecting to StreamPings endpoint")

	stream, err := s.client.StreamPings(ctx, connect.NewRequest(&agentv1.StreamPingsRequest{
		AgentId: s.agentID,
	}))
	if err != nil {
		return fmt.Errorf("failed to start stream: %w", err)
	}

	s.stream = stream
	s.retryAttempts = 0
	s.logger.Info("StreamPings connection established")
	return nil
}

func (s *StreamPingSource) Next(ctx context.Context) (*PingEvent, error) {
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
				return s.Next(ctx)
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	if !s.stream.Receive() {
		err := s.stream.Err()
		s.logger.Warn("Stream receive error, reconnecting: %v", err)
		s.stream.Close()
		s.stream = nil
		return s.Next(ctx)
	}

	msg := s.stream.Msg()

	event := &PingEvent{}

	if msg.Action != nil {
		switch *msg.Action {
		case agentv1.PingAction_PING_ACTION_IDLE:
			event.Action = "idle"
		case agentv1.PingAction_PING_ACTION_PAUSE:
			event.Action = "pause"
		case agentv1.PingAction_PING_ACTION_DISCONNECT:
			event.Action = "disconnect"
		}
	}

	if msg.Job != nil {
		event.Job = &api.Job{ID: msg.Job.Id}
	}

	s.logger.Debug("Received ping from stream: action=%s, job=%v", event.Action, event.Job != nil)

	return event, nil
}

func (s *StreamPingSource) Close() error {
	if s.stream != nil {
		s.stream.Close()
		s.stream = nil
	}
	return nil
}
