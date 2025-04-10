package clicommand

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
)

func TestStepCancel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(`{"uuid": "b0db1550-e68c-428f-9b4d-edf5599b2cff"}`))
		}))

		cfg := StepCancelConfig{
			ForceGracePeriodSeconds: 10,
			Force:                   true,
			Build:                   "1",
			StepOrKey:               "some-random-key",
			APIConfig: APIConfig{
				AgentAccessToken: "agentaccesstoken",
				Endpoint:         server.URL,
			},
		}

		l := logger.NewBuffer()
		err := cancelStep(ctx, cfg, l)
		assert.Nil(t, err)
		assert.Contains(t, l.Messages, "[info] Successfully cancelled step: b0db1550-e68c-428f-9b4d-edf5599b2cff")
	})

	t.Run("failed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusBadRequest)
		}))

		cfg := StepCancelConfig{
			ForceGracePeriodSeconds: 10,
			Force:                   true,
			StepOrKey:               "some-random-key",
			APIConfig: APIConfig{
				AgentAccessToken: "agentaccesstoken",
				Endpoint:         server.URL,
			},
		}

		l := logger.NewBuffer()
		err := cancelStep(ctx, cfg, l)
		assert.Contains(t, err.Error(), "Failed to cancel step")
	})
}
