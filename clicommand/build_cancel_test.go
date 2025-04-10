package clicommand

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
)

func TestBuildCancel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(`{"status": "canceled", "uuid": "1"}`))
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
		assert.Nil(t, err)
		assert.Contains(t, l.Messages, fmt.Sprintf("[info] Successfully cancelled build %s", cfg.Build))
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
		assert.NotNil(t, err)
	})
}
