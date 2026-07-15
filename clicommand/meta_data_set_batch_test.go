package clicommand

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func stubAPIClient(l logger.Logger, rt roundTripperFunc) *api.Client {
	return api.NewClient(l, api.Config{
		Endpoint:   "http://api.stub",
		Token:      "agentaccesstoken",
		HTTPClient: &http.Client{Transport: rt},
	})
}

func stubResponse(req *http.Request, status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
		Request:    req,
	}
}

func TestSetMetaDataBatch(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		l := logger.NewBuffer()

		var receivedBatch api.MetaDataBatch
		client := stubAPIClient(l, func(req *http.Request) (*http.Response, error) {
			if req.Method != "POST" {
				t.Errorf("req.Method = %q, want %q", req.Method, "POST")
			}
			if want := "/jobs/jobid/data/set-batch"; req.URL.Path != want {
				t.Errorf("req.URL.Path = %q, want %q", req.URL.Path, want)
			}
			if err := json.NewDecoder(req.Body).Decode(&receivedBatch); err != nil {
				t.Errorf("decoding request body: %v", err)
			}
			return stubResponse(req, http.StatusNoContent), nil
		})

		items := []api.MetaData{
			{Key: "foo", Value: "bar"},
			{Key: "baz", Value: "qux"},
		}

		if err := setMetaDataBatch(t.Context(), client, "jobid", l, items); err != nil {
			t.Fatalf("setMetaDataBatch error = %v, want nil", err)
		}
		if diff := cmp.Diff(receivedBatch.Items, items); diff != "" {
			t.Errorf("receivedBatch.Items diff (-got +want):\n%s", diff)
		}
	})

	t.Run("server error gives up when context is cancelled", func(t *testing.T) {
		t.Parallel()

		l := logger.NewBuffer()
		client := stubAPIClient(l, func(req *http.Request) (*http.Response, error) {
			return stubResponse(req, http.StatusInternalServerError), nil
		})

		items := []api.MetaData{{Key: "a", Value: "1"}}

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		err := setMetaDataBatch(ctx, client, "jobid", l, items)
		if err == nil {
			t.Fatal("setMetaDataBatch error = nil, want error")
		}
		if want := "failed to set meta-data batch"; !strings.Contains(err.Error(), want) {
			t.Errorf("setMetaDataBatch error = %q, want error containing %q", err, want)
		}
	})

	t.Run("401 does not retry", func(t *testing.T) {
		t.Parallel()

		l := logger.NewBuffer()
		callCount := 0
		client := stubAPIClient(l, func(req *http.Request) (*http.Response, error) {
			callCount++
			return stubResponse(req, http.StatusUnauthorized), nil
		})

		items := []api.MetaData{{Key: "a", Value: "1"}}

		if err := setMetaDataBatch(t.Context(), client, "jobid", l, items); err == nil {
			t.Fatal("setMetaDataBatch error = nil, want error")
		}
		if callCount != 1 {
			t.Errorf("callCount = %d, want 1", callCount)
		}
	})

	t.Run("404 does not retry", func(t *testing.T) {
		t.Parallel()

		l := logger.NewBuffer()
		callCount := 0
		client := stubAPIClient(l, func(req *http.Request) (*http.Response, error) {
			callCount++
			return stubResponse(req, http.StatusNotFound), nil
		})

		items := []api.MetaData{{Key: "a", Value: "1"}}

		if err := setMetaDataBatch(t.Context(), client, "jobid", l, items); err == nil {
			t.Fatal("setMetaDataBatch error = nil, want error")
		}
		if callCount != 1 {
			t.Errorf("callCount = %d, want 1", callCount)
		}
	})
}
