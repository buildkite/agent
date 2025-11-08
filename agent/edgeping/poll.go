package edgeping

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
)

type PollPingSource struct {
	pingFunc     func(context.Context) (*api.Ping, *api.Response, error)
	logger       logger.Logger
	interval     time.Duration
	ticker       *time.Ticker
	firstPing    chan struct{}
	testTrigger  chan struct{}
}

func NewPollPingSource(
	pingFunc func(context.Context) (*api.Ping, *api.Response, error),
	interval time.Duration,
	logger logger.Logger,
	noWaitForTesting bool,
) *PollPingSource {
	var testTrigger chan struct{}
	if noWaitForTesting {
		testTrigger = make(chan struct{})
		close(testTrigger)
	}

	first := make(chan struct{}, 1)
	first <- struct{}{}

	return &PollPingSource{
		pingFunc:    pingFunc,
		logger:      logger,
		interval:    interval,
		ticker:      time.NewTicker(interval),
		firstPing:   first,
		testTrigger: testTrigger,
	}
}

func (p *PollPingSource) Next(ctx context.Context) (*PingEvent, error) {
	select {
	case <-p.testTrigger:
	case <-p.firstPing:
	case <-p.ticker.C:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	jitter := rand.N(p.interval)
	p.logger.Debug("Jittering for %v before ping", jitter)
	
	select {
	case <-p.testTrigger:
	case <-time.After(jitter):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	ping, resp, err := p.pingFunc(ctx)
	if err != nil {
		if resp != nil && !api.IsRetryableStatus(resp) {
			return nil, fmt.Errorf("non-retryable ping error: %w", err)
		}
		return nil, fmt.Errorf("ping error: %w", err)
	}

	event := &PingEvent{}
	if ping != nil {
		event.Action = ping.Action
		event.Job = ping.Job
	}

	return event, nil
}

func (p *PollPingSource) Close() error {
	if p.ticker != nil {
		p.ticker.Stop()
	}
	return nil
}
