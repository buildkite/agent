package agent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisconnect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/disconnect":
			rw.WriteHeader(http.StatusOK)
			fmt.Fprintf(rw, `{"id": "fakeuuid", "connection_state": "disconnected"}`)
		default:
			t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	l := logger.NewBuffer()

	worker := &AgentWorker{
		logger:             l,
		agent:              nil,
		apiClient:          client,
		agentConfiguration: AgentConfiguration{},
		retrySleepFunc: func(time.Duration) {
			t.Error("unexpected retrier sleep")
		},
	}

	err := worker.Disconnect()
	require.NoError(t, err)

	assert.Equal(t, []string{"[info] Disconnecting...", "[info] Disconnected"}, l.Messages)
}

func TestDisconnectRetry(t *testing.T) {
	tries := 0
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/disconnect":
			if tries < 2 { // three failures before success
				rw.WriteHeader(http.StatusInternalServerError)
				tries++
			} else {
				rw.WriteHeader(http.StatusOK)
				fmt.Fprintf(rw, `{"id": "fakeuuid", "connection_state": "disconnected"}`)
			}
		default:
			t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	l := logger.NewBuffer()

	retrySleeps := make([]time.Duration, 0)
	retrySleepFunc := func(d time.Duration) {
		retrySleeps = append(retrySleeps, d)
	}

	worker := &AgentWorker{
		logger:             l,
		agent:              nil,
		apiClient:          client,
		agentConfiguration: AgentConfiguration{},
		retrySleepFunc:     retrySleepFunc,
	}

	err := worker.Disconnect()
	assert.NoError(t, err)

	// 2 failed attempts sleep 1 second each
	assert.Equal(t, []time.Duration{1 * time.Second, 1 * time.Second}, retrySleeps)

	require.Equal(t, 4, len(l.Messages))
	assert.Equal(t, "[info] Disconnecting...", l.Messages[0])
	assert.Regexp(t, regexp.MustCompile(`\[warn\] POST http.*/disconnect: 500 \(Attempt 1/4`), l.Messages[1])
	assert.Regexp(t, regexp.MustCompile(`\[warn\] POST http.*/disconnect: 500 \(Attempt 2/4`), l.Messages[2])
	assert.Equal(t, "[info] Disconnected", l.Messages[3])
}
