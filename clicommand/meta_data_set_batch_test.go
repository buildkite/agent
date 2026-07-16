package clicommand

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-cmp/cmp"
)

func TestParseMetaDataBatchArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    []api.MetaData
		wantErr error
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
			wantErr: invalidFormatError{arg: "foobar"},
		},
		{
			name:    "empty key",
			args:    []string{"=bar"},
			wantErr: emptyKeyError{arg: "=bar"},
		},
		{
			name:    "whitespace-only key",
			args:    []string{"  =bar"},
			wantErr: emptyKeyError{arg: "  =bar"},
		},
		{
			name:    "empty value",
			args:    []string{"foo="},
			wantErr: emptyValueError{arg: "foo="},
		},
		{
			name:    "whitespace-only value",
			args:    []string{"foo=   "},
			wantErr: emptyValueError{arg: "foo=   "},
		},
		{
			name:    "error on first invalid stops parsing",
			args:    []string{"a=1", "bad", "c=3"},
			wantErr: invalidFormatError{arg: "bad"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseMetaDataBatchArgs(tc.args)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("parseMetaDataBatchArgs(%v) error = %v, want %v", tc.args, err, tc.wantErr)
			}
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Fatalf("parseMetaDataBatchArgs(%v) diff (-got +want):\n%s", tc.args, diff)
			}
		})
	}
}

func TestSetMetaDataBatch(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		var receivedBatch api.MetaDataBatch
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.Method != "POST" {
				t.Errorf("req.Method = %q, want %q", req.Method, "POST")
			}
			if want := "/jobs/jobid/data/set-batch"; req.URL.Path != want {
				t.Errorf("req.URL.Path = %q, want %q", req.URL.Path, want)
			}
			if err := json.NewDecoder(req.Body).Decode(&receivedBatch); err != nil {
				t.Errorf("decoding request body: %v", err)
			}
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
		if err := setMetaDataBatch(t.Context(), cfg, l, items); err != nil {
			t.Fatalf("setMetaDataBatch error = %v, want nil", err)
		}
		if diff := cmp.Diff(receivedBatch.Items, items); diff != "" {
			t.Errorf("receivedBatch.Items diff (-got +want):\n%s", diff)
		}
	})

	t.Run("server error gives up when context is cancelled", func(t *testing.T) {
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

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		l := logger.NewBuffer()
		err := setMetaDataBatch(ctx, cfg, l, items)
		if err == nil {
			t.Fatal("setMetaDataBatch error = nil, want error")
		}
		if want := "failed to set meta-data batch"; !strings.Contains(err.Error(), want) {
			t.Errorf("setMetaDataBatch error = %q, want error containing %q", err, want)
		}
	})

	t.Run("401 does not retry", func(t *testing.T) {
		var callCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			callCount.Add(1)
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
		if err := setMetaDataBatch(t.Context(), cfg, l, items); err == nil {
			t.Fatal("setMetaDataBatch error = nil, want error")
		}
		if callCount.Load() != 1 {
			t.Errorf("callCount.Load() = %d, want 1", callCount.Load())
		}
	})

	t.Run("404 does not retry", func(t *testing.T) {
		var callCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			callCount.Add(1)
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
		if err := setMetaDataBatch(t.Context(), cfg, l, items); err == nil {
			t.Fatal("setMetaDataBatch error = nil, want error")
		}
		if callCount.Load() != 1 {
			t.Errorf("callCount.Load() = %d, want 1", callCount.Load())
		}
	})
}
