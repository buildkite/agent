package clicommand

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v4/internal/logtest"
)

func TestBuildCancel(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"status": "canceled", "uuid": "1"}`))
		}))

		cfg := BuildCancelConfig{
			Build: "1",
			APIConfig: APIConfig{
				AgentAccessToken: "agentaccesstoken",
				Endpoint:         server.URL,
			},
		}

		l, lh := logtest.NewLogger()
		err := cancelBuild(ctx, cfg, l)
		if got := err; got != nil {
			t.Errorf("cancelBuild(ctx, cfg, l) = %v, want nil", got)
		}
		if got, ok := findLogAttr(lh.Records(), "Successfully cancelled build", "build_id"); !ok || got != cfg.Build {
			t.Errorf("build_id attr = %v, %t, want %q, true", got, ok, cfg.Build)
		}
	})

	t.Run("failed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusInternalServerError)
		}))

		cfg := BuildCancelConfig{
			Build: "1",
			APIConfig: APIConfig{
				AgentAccessToken: "agentaccesstoken",
				Endpoint:         server.URL,
			},
		}

		l := slog.New(slog.DiscardHandler)
		err := cancelBuild(ctx, cfg, l)
		if got := err; got == nil {
			t.Errorf("cancelBuild(ctx, cfg, l) = %v, want non-nil value", got)
		}
	})
}
