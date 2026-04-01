package clicommand

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
)

func TestParseMetaDataBatchArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    []api.MetaData
		wantErr string
	}{
		{
			name: "single pair",
			args: []string{"foo=bar"},
			want: []api.MetaData{{Key: "foo", Value: "bar"}},
		},
		{
			name: "multiple pairs",
			args: []string{"a=1", "b=2", "c=3"},
			want: []api.MetaData{
				{Key: "a", Value: "1"},
				{Key: "b", Value: "2"},
				{Key: "c", Value: "3"},
			},
		},
		{
			name: "value containing equals sign",
			args: []string{"key=val=ue"},
			want: []api.MetaData{{Key: "key", Value: "val=ue"}},
		},
		{
			name:    "missing equals sign",
			args:    []string{"foobar"},
			wantErr: `invalid argument "foobar": must be in key=value format`,
		},
		{
			name:    "empty key",
			args:    []string{"=bar"},
			wantErr: `invalid argument "=bar": key cannot be empty`,
		},
		{
			name:    "whitespace-only key",
			args:    []string{"  =bar"},
			wantErr: `invalid argument "  =bar": key cannot be empty`,
		},
		{
			name:    "empty value",
			args:    []string{"foo="},
			wantErr: `invalid argument "foo=": value cannot be empty`,
		},
		{
			name:    "whitespace-only value",
			args:    []string{"foo=   "},
			wantErr: `invalid argument "foo=   ": value cannot be empty`,
		},
		{
			name:    "error on first invalid stops parsing",
			args:    []string{"a=1", "bad", "c=3"},
			wantErr: `invalid argument "bad": must be in key=value format`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseMetaDataBatchArgs(tc.args)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSetMetaDataBatch(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		var receivedBatch api.MetaDataBatch
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			assert.Equal(t, "POST", req.Method)
			assert.Equal(t, "/jobs/jobid/data/set-batch", req.URL.Path)
			assert.NoError(t, json.NewDecoder(req.Body).Decode(&receivedBatch))
			rw.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		cfg := MetaDataSetBatchConfig{
			Job: "jobid",
			APIConfig: APIConfig{
				AgentAccessToken: "agentaccesstoken",
				Endpoint:         server.URL,
			},
		}

		items := []api.MetaData{
			{Key: "foo", Value: "bar"},
			{Key: "baz", Value: "qux"},
		}

		l := logger.NewBuffer()
		err := setMetaDataBatch(context.Background(), cfg, l, items)
		assert.NoError(t, err)
		assert.Equal(t, items, receivedBatch.Items)
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		cfg := MetaDataSetBatchConfig{
			Job: "jobid",
			APIConfig: APIConfig{
				AgentAccessToken: "agentaccesstoken",
				Endpoint:         server.URL,
			},
		}

		items := []api.MetaData{{Key: "a", Value: "1"}}

		l := logger.NewBuffer()
		err := setMetaDataBatch(context.Background(), cfg, l, items)
		assert.ErrorContains(t, err, "failed to set meta-data batch")
	})

	t.Run("401 does not retry", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			callCount++
			rw.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		cfg := MetaDataSetBatchConfig{
			Job: "jobid",
			APIConfig: APIConfig{
				AgentAccessToken: "agentaccesstoken",
				Endpoint:         server.URL,
			},
		}

		items := []api.MetaData{{Key: "a", Value: "1"}}

		l := logger.NewBuffer()
		err := setMetaDataBatch(context.Background(), cfg, l, items)
		assert.Error(t, err)
		assert.Equal(t, 1, callCount)
	})

	t.Run("404 does not retry", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			callCount++
			rw.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		cfg := MetaDataSetBatchConfig{
			Job: "jobid",
			APIConfig: APIConfig{
				AgentAccessToken: "agentaccesstoken",
				Endpoint:         server.URL,
			},
		}

		items := []api.MetaData{{Key: "a", Value: "1"}}

		l := logger.NewBuffer()
		err := setMetaDataBatch(context.Background(), cfg, l, items)
		assert.Error(t, err)
		assert.Equal(t, 1, callCount)
	})

	t.Run("validation error from server", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusUnprocessableEntity)
			rw.Write([]byte(`{"message": "key cannot be blank"}`))
		}))
		defer server.Close()

		cfg := MetaDataSetBatchConfig{
			Job: "jobid",
			APIConfig: APIConfig{
				AgentAccessToken: "agentaccesstoken",
				Endpoint:         server.URL,
			},
		}

		items := []api.MetaData{{Key: "a", Value: "1"}}

		l := logger.NewBuffer()
		err := setMetaDataBatch(context.Background(), cfg, l, items)
		assert.Error(t, err)
	})
}
