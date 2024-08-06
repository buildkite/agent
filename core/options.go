package core

import (
	"time"

	"github.com/buildkite/agent/v3/logger"
)

type controllerConfig struct {
	logger         logger.Logger
	retrySleepFunc func(time.Duration)

	// Controller creation options - ignored for more specific functions.
	endpoint          string
	userAgent         string
	debugHTTP         bool
	allowHTTP2        bool
	priority          string
	scriptEvalEnabled bool
}

// ControllerOption is a functional option for setting optional behaviour.
type ControllerOption func(*controllerConfig)

// WithLogger enables logging through a particular logger.
// Defaults to [logger.Discard].
func WithLogger(l logger.Logger) ControllerOption {
	return func(c *controllerConfig) {
		c.logger = l
	}
}

// WithRetrySleepFunc is used to override the inter-retry sleep in roko.
// This is mainly useful for unit tests. Defaults to nil (default sleep).
func WithRetrySleepFunc(f func(time.Duration)) ControllerOption {
	return func(c *controllerConfig) {
		c.retrySleepFunc = f
	}
}

// WithEndpoint allows overriding the API endpoint (base URL).
// Defaults to "https://agent.buildkite.com/v3".
func WithEndpoint(endpoint string) ControllerOption {
	return func(c *controllerConfig) {
		c.endpoint = endpoint
	}
}

// WithUserAgent allows overriding the user agent.
// Defaults to the value retuned from [version.UserAgent].
func WithUserAgent(userAgent string) ControllerOption {
	return func(c *controllerConfig) {
		c.userAgent = userAgent
	}
}

// WithDebugHTTP can be used to enable HTTP debug logs.
// Only applies to agent creation. Defaults to false.
func WithDebugHTTP(debug bool) ControllerOption {
	return func(c *controllerConfig) {
		c.debugHTTP = debug
	}
}

// WithAllowHTTP can be used to change whether HTTP/2 is allowed.
// Only applies to agent creation. Defaults to true.
func WithAllowHTTP2(allow bool) ControllerOption {
	return func(c *controllerConfig) {
		c.allowHTTP2 = allow
	}
}

// WithPriority sets the agent priority value. Defaults to the empty string.
func WithPriority(priority string) ControllerOption {
	return func(c *controllerConfig) {
		c.priority = priority
	}
}

// WithScriptEvalEnabled sets the ScriptEvalEnabled registration parameter.
// Defaults to true.
func WithScriptEvalEnabled(enabled bool) ControllerOption {
	return func(c *controllerConfig) {
		c.scriptEvalEnabled = enabled
	}
}
