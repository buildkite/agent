package core

import (
	"context"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/version"
)

// Controller is a client for the Buildkite Agent API. It is useful for
// implementing completely custom job behaviour within process (instead of
// having to execute hooks).
type Controller struct {
	client *Client
	config *controllerConfig
}

// NewController creates and registers a new agent with Buildkite.
// Currently, only Acquire Job is supported.
// ctx is only used for agent registration and connection.
func NewController(ctx context.Context, regToken, agentName string, tags []string, opts ...ControllerOption) (*Controller, error) {
	// Some of these are redundant by Go zero-value defaults, but it spells out
	// what the defaults are.
	cfg := &controllerConfig{
		logger:            logger.Discard,
		retrySleepFunc:    nil,
		endpoint:          "https://agent.buildkite.com/v3",
		userAgent:         version.UserAgent(),
		debugHTTP:         false,
		allowHTTP2:        true,
		priority:          "",
		scriptEvalEnabled: true,
	}
	for _, o := range opts {
		o(cfg)
	}

	apiClient := api.NewClient(cfg.logger, api.Config{
		Token: regToken,

		Endpoint:     cfg.endpoint,
		UserAgent:    cfg.userAgent,
		DisableHTTP2: !cfg.allowHTTP2,
		DebugHTTP:    cfg.debugHTTP,
	})

	controller := &Controller{
		config: cfg,
		client: &Client{
			APIClient:      apiClient,
			Logger:         cfg.logger,
			RetrySleepFunc: cfg.retrySleepFunc,
		},
	}

	reg, err := controller.client.Register(ctx, api.AgentRegisterRequest{
		Name:               agentName,
		IgnoreInDispatches: true, // TODO: implement a regular agent mode? (ping loop, accept job, etc)
		ScriptEvalEnabled:  cfg.scriptEvalEnabled,
		Priority:           cfg.priority,
		Tags:               tags,
		Features:           []string{"agent-core"},
	})
	if err != nil {
		return nil, err
	}
	controller.client.APIClient = apiClient.FromAgentRegisterResponse(reg)

	if err := controller.client.Connect(ctx); err != nil {
		return nil, err
	}

	return controller, nil
}

// Close disconnects the agent.
func (a *Controller) Close(ctx context.Context) error {
	return a.client.Disconnect(ctx)
}

// AcquireJob acquires a specific job from Buildkite.
// It doesn't run the job - the caller is responsible for that.
func (a *Controller) AcquireJob(ctx context.Context, jobID string) (*api.Job, error) {
	return a.client.AcquireJob(ctx, jobID)
}

// NewJobController creates a new job controller for a job.
func (a *Controller) NewJobController(job *api.Job) *JobController {
	return NewJobController(a.client, job)
}
