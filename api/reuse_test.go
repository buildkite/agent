package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
)

func TestReuseEndpoints(t *testing.T) {
	t.Parallel()

	type request struct {
		Method, Path, Query string
		Body                map[string]any
	}
	var got []request

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var parsed map[string]any
		if len(body) > 0 {
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Errorf("json.Unmarshal(%q) error = %v", body, err)
			}
		}
		got = append(got, request{req.Method, req.URL.Path, req.URL.RawQuery, parsed})

		switch req.Method + " " + req.URL.Path {
		case "GET /jobs/j1/reuse":
			_, _ = io.WriteString(rw, `{"step_key":"build","save":true,"save_disabled_reason":null,
				"files":{"include":["web/**"],"exclude":[],"untracked":false},
				"outputs":{"paths":["dist/"],"artifacts":["dist/**"],"values":["bundle_size"]},
				"restore":[{"step_key":"deps","download":false,
					"paths":[{"path":"node_modules/","digest":"b3:aa","key":"step-reuse/aa.tar.gz","size":123,"files":40}]}]}`)
		case "POST /jobs/j1/reuse/result":
			_, _ = io.WriteString(rw, `{"saved":true,"reason":"ok","action_key":"hs256:abc"}`)
		case "POST /jobs/j1/reuse/input_unavailable":
			_, _ = io.WriteString(rw, `{"repair":true,"message":"repairing"}`)
		case "PUT /jobs/j1/outputs/values":
			rw.WriteHeader(http.StatusCreated)
		case "GET /jobs/j1/outputs/values":
			if req.URL.Query().Get("name") == "missing" {
				http.Error(rw, `{"message":"not found"}`, http.StatusNotFound)
				return
			}
			_, _ = io.WriteString(rw, `{"value":"184kB"}`)
		default:
			http.Error(rw, `{"message":"unexpected"}`, http.StatusTeapot)
		}
	}))
	t.Cleanup(server.Close)

	client := api.NewClient(logger.Discard, api.Config{Endpoint: server.URL, Token: "token"})
	ctx := t.Context()

	plan, _, err := client.GetReusePlan(ctx, "j1")
	if err != nil {
		t.Fatalf("GetReusePlan() error = %v", err)
	}
	wantPlan := &api.ReusePlan{
		StepKey: "build",
		Save:    true,
		Files:   &api.ReuseFiles{Include: []string{"web/**"}, Exclude: []string{}},
		Outputs: api.ReuseOutputs{Paths: []string{"dist/"}, Artifacts: []string{"dist/**"}, Values: []string{"bundle_size"}},
		Restore: []api.ReuseRestore{{
			StepKey: "deps",
			Paths:   []api.ReusePath{{Path: "node_modules/", Digest: "b3:aa", Key: "step-reuse/aa.tar.gz", Size: 123, Files: 40}},
		}},
	}
	if diff := cmp.Diff(plan, wantPlan); diff != "" {
		t.Errorf("GetReusePlan() (-got +want):\n%s", diff)
	}

	digest := "b3:ff"
	res, _, err := client.SubmitReuseResult(ctx, "j1", &api.ReuseResult{
		ExitStatus:          0,
		VerifiedFilesDigest: &digest,
		Paths:               []api.ReusePath{{Path: "dist/", Digest: "b3:bb", Key: "step-reuse/bb.tar.gz", Size: 9, Files: 4}},
		OverheadMS:          1234,
	})
	if err != nil {
		t.Fatalf("SubmitReuseResult() error = %v", err)
	}
	if diff := cmp.Diff(res, &api.ReuseResultResponse{Saved: true, Reason: "ok", ActionKey: "hs256:abc"}); diff != "" {
		t.Errorf("SubmitReuseResult() (-got +want):\n%s", diff)
	}

	unavail, _, err := client.ReportReuseInputUnavailable(ctx, "j1", &api.ReuseInputUnavailable{StepKey: "deps", Path: "node_modules/", Reason: "blob not found"})
	if err != nil {
		t.Fatalf("ReportReuseInputUnavailable() error = %v", err)
	}
	if diff := cmp.Diff(unavail, &api.ReuseInputUnavailableResponse{Repair: true, Message: "repairing"}); diff != "" {
		t.Errorf("ReportReuseInputUnavailable() (-got +want):\n%s", diff)
	}

	if _, err := client.SetOutputValue(ctx, "j1", &api.OutputValue{Name: "bundle_size", Value: "184kB"}); err != nil {
		t.Fatalf("SetOutputValue() error = %v", err)
	}

	v, _, err := client.GetOutputValue(ctx, "j1", "build", "bundle_size")
	if err != nil {
		t.Fatalf("GetOutputValue() error = %v", err)
	}
	if v.Value != "184kB" {
		t.Errorf("GetOutputValue() value = %q, want 184kB", v.Value)
	}

	_, resp, err := client.GetOutputValue(ctx, "j1", "build", "missing")
	if err == nil || resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Errorf("GetOutputValue(missing) = (%v, %v), want 404 error", resp, err)
	}

	want := []request{
		{"GET", "/jobs/j1/reuse", "", nil},
		{"POST", "/jobs/j1/reuse/result", "", map[string]any{
			"exit_status": 0.0, "verified_files_digest": "b3:ff", "overhead_ms": 1234.0, "discard_reason": nil,
			"paths": []any{map[string]any{"path": "dist/", "digest": "b3:bb", "key": "step-reuse/bb.tar.gz", "size": 9.0, "files": 4.0}},
		}},
		{"POST", "/jobs/j1/reuse/input_unavailable", "", map[string]any{"step_key": "deps", "path": "node_modules/", "reason": "blob not found"}},
		{"PUT", "/jobs/j1/outputs/values", "", map[string]any{"name": "bundle_size", "value": "184kB"}},
		{"GET", "/jobs/j1/outputs/values", "name=bundle_size&step=build", nil},
		{"GET", "/jobs/j1/outputs/values", "name=missing&step=build", nil},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("requests (-got +want):\n%s", diff)
	}
}
