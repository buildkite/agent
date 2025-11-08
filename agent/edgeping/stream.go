package edgeping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/launchdarkly/eventsource"
)

var ErrStreamUnavailable = errors.New("streaming ping unavailable")

type StreamPingSource struct {
	logger        logger.Logger
	agentID       string
	streamURL     string
	stream        *eventsource.Stream
	retryAttempts int
	maxRetries    int
	httpClient    *http.Client
}

type sseMessage struct {
	Action   string `json:"action"`
	Message  string `json:"message,omitempty"`
	JobID    string `json:"job_id,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

func streamEndpointFromAgentEndpoint(agentEndpoint string, agentID string) string {
	u, err := url.Parse(agentEndpoint)
	if err != nil {
		return fmt.Sprintf("%s/stream?agent_id=%s", agentEndpoint, url.QueryEscape(agentID))
	}

	u.Path = u.Path + "/stream"
	q := u.Query()
	q.Set("agent_id", agentID)
	u.RawQuery = q.Encode()

	return u.String()
}

func NewStreamPingSource(agentEndpoint string, agentID string, logger logger.Logger) *StreamPingSource {
	streamURL := streamEndpointFromAgentEndpoint(agentEndpoint, agentID)

	return &StreamPingSource{
		logger:     logger,
		agentID:    agentID,
		streamURL:  streamURL,
		maxRetries: 5,
		httpClient: &http.Client{
			Timeout: 0, // No timeout for SSE connections
		},
	}
}

func (s *StreamPingSource) connect(ctx context.Context) error {
	s.logger.Info("Connecting to StreamPings endpoint at %s", s.streamURL)

	req, err := http.NewRequestWithContext(ctx, "GET", s.streamURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	stream, err := eventsource.SubscribeWithRequest("", req)
	if err != nil {
		return fmt.Errorf("failed to start stream: %w", err)
	}

	s.stream = stream
	s.retryAttempts = 0
	s.logger.Info("StreamPings connection established")
	return nil
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

		select {
		case <-ctx.Done():
			s.logger.Info("Context cancelled, closing stream")
			s.Close()
			return nil, ctx.Err()

		case sseEvent, ok := <-s.stream.Events:
			if !ok {
				s.logger.Warn("Stream closed by server, reconnecting")
				s.stream = nil
				continue
			}

			var msg sseMessage
			if err := json.Unmarshal([]byte(sseEvent.Data()), &msg); err != nil {
				s.logger.Warn("Failed to parse SSE message: %v, data: %s", err, sseEvent.Data())
				continue
			}

			event := &PingEvent{
				Action: msg.Action,
			}

			if msg.JobID != "" {
				event.Job = &api.Job{ID: msg.JobID}
			}

			s.logger.Debug("Received ping from stream: action=%s, job=%v", event.Action, event.Job != nil)

			return event, nil

		case err, ok := <-s.stream.Errors:
			if !ok {
				s.logger.Warn("Error channel closed, reconnecting")
				s.stream = nil
				continue
			}

			s.logger.Warn("Stream error, reconnecting: %v", err)
			s.Close()
			s.stream = nil
			continue
		}
	}
}

func (s *StreamPingSource) Close() error {
	if s.stream != nil {
		s.stream.Close()
		s.stream = nil
	}
	return nil
}
