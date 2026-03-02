package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/buildkite/agent/v3/jobapi"
	"github.com/buildkite/bintest/v3"
)

func TestBootstrapRunsJobAPI(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	tester.ExpectGlobalHook("command").Once().AndCallFunc(func(c *bintest.Call) {
		socketPath := c.GetEnv("BUILDKITE_AGENT_JOB_API_SOCKET")
		if socketPath == "" {
			t.Errorf("Expected BUILDKITE_AGENT_JOB_API_SOCKET to be set")
			c.Exit(1)
			return
		}

		socketToken := c.GetEnv("BUILDKITE_AGENT_JOB_API_TOKEN")
		if socketToken == "" {
			t.Errorf("Expected BUILDKITE_AGENT_JOB_API_TOKEN to be set")
			c.Exit(1)
			return
		}

		client := &http.Client{
			Transport: &http.Transport{
				DialContext: func(context.Context, string, string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
		}

		req, err := http.NewRequest(http.MethodGet, "http://job-executor/api/current-job/v0/env", nil)
		if err != nil {
			t.Errorf("creating request: %v", err)
			c.Exit(1)
			return
		}

		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", socketToken))

		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("sending request: %v", err)
			c.Exit(1)
			return
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("expected status 200, got %d, body: %s", resp.StatusCode, body)
			c.Exit(1)
			return
		}

		var envResp jobapi.EnvGetResponse
		err = json.NewDecoder(resp.Body).Decode(&envResp)
		if err != nil {
			t.Errorf("decoding env get response: %v", err)
			c.Exit(1)
			return
		}

		for name, val := range envResp.Env {
			if val != c.GetEnv(name) {
				t.Errorf("expected c.GetEnv(%q) = %s, got %s", name, c.GetEnv(name), val)
				c.Exit(1)
				return
			}
		}

		mtn := "chimborazo"
		b, err := json.Marshal(jobapi.EnvUpdateRequest{Env: map[string]string{"MOUNTAIN": mtn}})
		if err != nil {
			t.Errorf("marshaling env update request: %v", err)
			c.Exit(1)
			return
		}

		req, err = http.NewRequest(http.MethodPatch, "http://job-executor/api/current-job/v0/env", bytes.NewBuffer(b))
		if err != nil {
			t.Errorf("creating patch request: %v", err)
			c.Exit(1)
			return
		}

		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", socketToken))
		resp, err = client.Do(req)
		if err != nil {
			t.Errorf("sending patch request: %v", err)
			c.Exit(1)
			return
		}
		defer resp.Body.Close() //nolint:errcheck // response body close errors are inconsequential in tests

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("expected status 200, got %d, body: %s", resp.StatusCode, body)
			c.Exit(1)
			return
		}

		var patchResp jobapi.EnvUpdateResponse
		err = json.NewDecoder(resp.Body).Decode(&patchResp)
		if err != nil {
			t.Errorf("decoding env get response: %v", err)
			c.Exit(1)
			return
		}

		if patchResp.Added[0] != "MOUNTAIN" {
			t.Errorf("expected patchResp.Added[0] = %q, got %s", mtn, patchResp.Added[0])
			c.Exit(1)
			return
		}

		c.Exit(0)
	})

	tester.ExpectGlobalHook("post-command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("MOUNTAIN"), "chimborazo"; got != want {
			fmt.Fprintf(c.Stderr, "MOUNTAIN = %q, want %q\n", got, want) //nolint:errcheck // test helper; write error is non-actionable
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.RunAndCheck(t)
}
