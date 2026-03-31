package clicommand

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/buildkite/agent/v4/logger"
)

func TestBuildCancel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

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

		l := logger.NewBuffer()
		err := cancelBuild(ctx, cfg, l)
		if got := err; got != nil {
			t.Errorf("cancelBuild(ctx, cfg, l) = %v, want nil", got)
		}
		if got, want := l.Messages, fmt.Sprintf("[info] Successfully cancelled build %s", cfg.Build); !slices.Contains(got, want) {
			t.Errorf("l.Messages = %v, want containing %q", got, want)
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

		l := logger.NewBuffer()
		err := cancelBuild(ctx, cfg, l)
		if got := err; got == nil {
			t.Errorf("cancelBuild(ctx, cfg, l) = %v, want non-nil value", got)
		}
	})
}
