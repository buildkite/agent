package clicommand

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/logger"
)

func TestStepCancel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"uuid": "b0db1550-e68c-428f-9b4d-edf5599b2cff"}`))
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
		if got := err; got != nil {
			t.Errorf("cancelStep(ctx, cfg, l) = %v, want nil", got)
		}
		if got, want := l.Messages, "[info] Successfully cancelled step: b0db1550-e68c-428f-9b4d-edf5599b2cff"; !slices.Contains(got, want) {
			t.Errorf("l.Messages = %v, want containing %q", got, want)
		}
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
		if got, want := err.Error(), "failed to cancel step"; !strings.Contains(got, want) {
			t.Errorf("err.Error() = %q, want containing %q", got, want)
		}
	})
}
