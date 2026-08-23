package clicommand

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/logtest"
)

func TestStepCancel(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

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

		l, lh := logtest.NewLogger()
		err := cancelStep(ctx, cfg, l)
		if got := err; got != nil {
			t.Errorf("cancelStep(ctx, cfg, l) = %v, want nil", got)
		}
		if got, ok := findLogAttr(lh.Records(), "Successfully cancelled step", "step_id"); !ok || got != "b0db1550-e68c-428f-9b4d-edf5599b2cff" {
			t.Errorf("step_id attr = %v, %t, want %q, true", got, ok, "b0db1550-e68c-428f-9b4d-edf5599b2cff")
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

		l := slog.New(slog.DiscardHandler)
		err := cancelStep(ctx, cfg, l)
		if got, want := err.Error(), "failed to cancel step"; !strings.Contains(got, want) {
			t.Errorf("err.Error() = %q, want containing %q", got, want)
		}
	})
}
