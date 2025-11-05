package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/logger"
	"github.com/gorilla/websocket"
)

const (
	nudgeReconnectDelay = 30 * time.Second
	nudgeWriteTimeout   = 10 * time.Second
	nudgePingInterval   = 30 * time.Second
	nudgePongWait       = 60 * time.Second
)

type NudgeWorker struct {
	logger      logger.Logger
	endpoint    string
	accessToken string
	conn        *websocket.Conn
	connMu      sync.Mutex
	nudgeChan   chan struct{}
	stopOnce    sync.Once
	stop        chan struct{}
}

func NewNudgeWorker(l logger.Logger, endpoint, accessToken string, nudgeChan chan struct{}) *NudgeWorker {
	return &NudgeWorker{
		logger:      l,
		endpoint:    endpoint,
		accessToken: accessToken,
		nudgeChan:   nudgeChan,
		stop:        make(chan struct{}),
	}
}

func (n *NudgeWorker) Start(ctx context.Context) {
	if !experiments.IsEnabled(ctx, experiments.AgentNudge) {
		n.logger.Debug("Agent nudge experiment not enabled, skipping nudge worker")
		return
	}

	n.logger.Info("Starting nudge worker")
	go n.run(ctx)
}

func (n *NudgeWorker) Stop() {
	n.stopOnce.Do(func() {
		close(n.stop)
		n.closeConnection()
	})
}

func (n *NudgeWorker) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			n.logger.Debug("Nudge worker stopping due to context cancellation")
			return
		case <-n.stop:
			n.logger.Debug("Nudge worker stopping")
			return
		default:
		}

		if err := n.connectAndListen(ctx); err != nil {
			n.logger.Warn("Nudge worker connection failed: %v", err)
		}

		reconnectDelay := nudgeReconnectDelay + time.Duration(rand.N(10*time.Second))
		n.logger.Debug("Nudge worker will reconnect in %v", reconnectDelay)

		select {
		case <-time.After(reconnectDelay):
		case <-ctx.Done():
			return
		case <-n.stop:
			return
		}
	}
}

func (n *NudgeWorker) connectAndListen(ctx context.Context) error {
	wsURL, err := n.buildWebSocketURL()
	if err != nil {
		return fmt.Errorf("failed to build WebSocket URL: %w", err)
	}

	n.logger.Debug("Connecting to nudge WebSocket at %s", wsURL)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	headers := http.Header{}
	headers.Set("Authorization", fmt.Sprintf("Token %s", n.accessToken))

	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return fmt.Errorf("failed to dial WebSocket: %w", err)
	}

	n.connMu.Lock()
	n.conn = conn
	n.connMu.Unlock()

	defer n.closeConnection()

	n.logger.Info("Connected to nudge WebSocket")

	errCh := make(chan error, 1)
	go n.readMessages(conn, errCh)
	go n.writePings(ctx, conn, errCh)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-n.stop:
		return nil
	}
	// Note: defer n.closeConnection() will run here, which unblocks
	// conn.ReadMessage() in readMessages, allowing that goroutine to exit
}

func (n *NudgeWorker) readMessages(conn *websocket.Conn, errCh chan error) {
	// Set up pong handler to reset the read deadline when we receive a pong
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(nudgePongWait))
	})

	// Set initial read deadline
	if err := conn.SetReadDeadline(time.Now().Add(nudgePongWait)); err != nil {
		errCh <- fmt.Errorf("failed to set initial read deadline: %w", err)
		return
	}

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			errCh <- fmt.Errorf("failed to read message: %w", err)
			return
		}

		if messageType == websocket.TextMessage {
			n.handleMessage(message)
		}
	}
}

func (n *NudgeWorker) writePings(ctx context.Context, conn *websocket.Conn, errCh chan error) {
	ticker := time.NewTicker(nudgePingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := conn.SetWriteDeadline(time.Now().Add(nudgeWriteTimeout)); err != nil {
				errCh <- fmt.Errorf("failed to set write deadline: %w", err)
				return
			}
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				errCh <- fmt.Errorf("failed to write ping: %w", err)
				return
			}
		case <-ctx.Done():
			return
		case <-n.stop:
			return
		}
	}
}

func (n *NudgeWorker) handleMessage(message []byte) {
	var data map[string]interface{}
	if err := json.Unmarshal(message, &data); err != nil {
		n.logger.Warn("Failed to parse nudge message: %v", err)
		return
	}

	n.logger.Debug("Received nudge message: %v", data)

	select {
	case n.nudgeChan <- struct{}{}:
		n.logger.Debug("Nudge signal sent to ping loop")
	default:
		n.logger.Debug("Nudge channel full, skipping")
	}
}

func (n *NudgeWorker) closeConnection() {
	n.connMu.Lock()
	defer n.connMu.Unlock()

	if n.conn != nil {
		n.conn.Close()
		n.conn = nil
	}
}

func (n *NudgeWorker) buildWebSocketURL() (string, error) {
	u, err := url.Parse(n.endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to parse endpoint: %w", err)
	}

	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}

	u.Path = strings.TrimSuffix(u.Path, "/") + "/nudge"

	return u.String(), nil
}
