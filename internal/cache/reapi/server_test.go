package reapi_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	repb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache/reapi"
	"github.com/buildkite/agent/v4/logger"
	bs "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// A metadata-only Agent API fixture. Transfers use the real file store adapter.
func cacheAPI(t *testing.T) (*api.Client, string) {
	t.Helper()
	var mu sync.Mutex
	entries := make(map[string]api.CacheEntryRetrieveResp)
	pending := make(map[string]api.CacheEntryRetrieveResp)
	key := func(paths []string, parts []api.CacheKeyPart) string {
		b, _ := json.Marshal([]any{paths, parts})
		return string(b)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Token job-api-token" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "denied"})
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/store"):
			var req api.CacheEntryCreateReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			for _, part := range req.CacheKey {
				if !part.Mandatory {
					t.Error("non-mandatory cache identity")
				}
			}
			id := fmt.Sprint(len(pending) + 1)
			pending[id] = api.CacheEntryRetrieveResp{
				TargetPaths:   req.TargetPaths,
				CacheKey:      req.CacheKey,
				Blobs:         req.Blobs,
				Store:         "agent_managed",
				RetentionDays: 7,
				UploadID:      id,
			}
			_ = json.NewEncoder(w).Encode(api.CacheEntryCreateResp{UploadID: id, RetentionDays: 7})
		case strings.HasSuffix(r.URL.Path, "/commit"):
			var req api.CacheEntryCommitReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			entry := pending[req.UploadID]
			entries[key(entry.TargetPaths, entry.CacheKey)] = entry
			_ = json.NewEncoder(w).Encode(api.CacheEntryCommitResp{})
		case strings.HasSuffix(r.URL.Path, "/retrieve"):
			var req api.CacheEntryRetrieveReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			entry, ok := entries[key(req.TargetPaths, req.CacheKey)]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": api.CacheEntryNotFound})
				return
			}
			_ = json.NewEncoder(w).Encode(entry)
		case strings.HasSuffix(r.URL.Path, "/confirm"):
			_ = json.NewEncoder(w).Encode(api.CacheEntryConfirmResp{})
		default:
			_ = json.NewEncoder(w).Encode(api.CacheRegistryResp{Store: "agent_managed"})
		}
	}))
	t.Cleanup(srv.Close)
	return api.NewClient(logger.Discard, api.Config{Endpoint: srv.URL + "/", Token: "job-api-token"}), "file://" + t.TempDir()
}

func connect(t *testing.T, b *reapi.Backend) (*reapi.Server, *grpc.ClientConn, context.Context, func()) {
	t.Helper()
	s, err := reapi.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	token := reapi.NewToken()
	unregister := s.Register(t.Context(), token, b)
	t.Cleanup(unregister)
	conn, err := grpc.NewClient(strings.TrimPrefix(s.Endpoint(), "http://"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return s, conn, metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer "+token), unregister
}

func digest(data []byte) *repb.Digest {
	h := sha256.Sum256(data)
	return &repb.Digest{Hash: hex.EncodeToString(h[:]), SizeBytes: int64(len(data))}
}

func TestCachePersistenceAndIsolation(t *testing.T) {
	t.Parallel()
	client, bucket := cacheAPI(t)
	b := &reapi.Backend{API: client, BucketURL: bucket}
	s, conn, ctx, unregister := connect(t, b)
	capabilities := repb.NewCapabilitiesClient(conn)
	if _, err := capabilities.GetCapabilities(t.Context(), &repb.GetCapabilitiesRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous request: %v", err)
	}
	caps, err := capabilities.GetCapabilities(ctx, &repb.GetCapabilitiesRequest{})
	if err != nil || caps.GetExecutionCapabilities().GetExecEnabled() || !caps.GetCacheCapabilities().GetActionCacheUpdateCapabilities().GetUpdateEnabled() {
		t.Fatalf("capabilities = %v, %v", caps, err)
	}
	execution, err := repb.NewExecutionClient(conn).Execute(ctx, &repb.ExecuteRequest{})
	if err == nil {
		_, err = execution.Recv()
	}
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("execution = %v, want UNIMPLEMENTED", err)
	}
	cas := repb.NewContentAddressableStorageClient(conn)
	payload := []byte("compiled output\x00with binary bytes\xff")
	d := digest(payload)
	write, err := cas.BatchUpdateBlobs(ctx, &repb.BatchUpdateBlobsRequest{
		InstanceName: "project",
		Requests: []*repb.BatchUpdateBlobsRequest_Request{
			{Digest: d, Data: payload},
			{Digest: digest([]byte("different")), Data: payload},
		},
	})
	if err != nil || write.Responses[0].Status == nil || write.Responses[0].Status.GetCode() != 0 || write.Responses[1].Status.GetCode() != int32(codes.InvalidArgument) {
		t.Fatalf("batch update = %v, %v", write, err)
	}
	missing := digest([]byte("not stored"))
	availability, err := cas.FindMissingBlobs(ctx, &repb.FindMissingBlobsRequest{
		InstanceName: "project",
		BlobDigests:  []*repb.Digest{d, missing},
	})
	if err != nil || len(availability.MissingBlobDigests) != 1 || !proto.Equal(availability.MissingBlobDigests[0], missing) {
		t.Fatalf("missing blobs = %v, %v", availability, err)
	}
	readBatch, err := cas.BatchReadBlobs(ctx, &repb.BatchReadBlobsRequest{
		InstanceName: "project",
		Digests:      []*repb.Digest{d, missing},
	})
	if err != nil || !bytes.Equal(readBatch.Responses[0].Data, payload) || readBatch.Responses[0].Status == nil || readBatch.Responses[1].Status.GetCode() != int32(codes.NotFound) {
		t.Fatalf("batch read = %v, %v", readBatch, err)
	}
	action := digest([]byte("build action"))
	want := &repb.ActionResult{
		ExitCode:    7,
		OutputFiles: []*repb.OutputFile{{Path: "out/bin", Digest: d, IsExecutable: true}},
		StdoutRaw:   []byte("stdout\n"),
		StderrRaw:   []byte("stderr\n"),
		ExecutionMetadata: &repb.ExecutedActionMetadata{
			AuxiliaryMetadata: []*anypb.Any{{
				TypeUrl: "type.googleapis.com/once.cache.v1.ActionResult",
				Value:   []byte(`{"native":"opaque"}`),
			}},
		},
	}
	ac := repb.NewActionCacheClient(conn)
	if _, err := ac.UpdateActionResult(ctx, &repb.UpdateActionResultRequest{
		InstanceName: "project",
		ActionDigest: action,
		ActionResult: want,
	}); err != nil {
		t.Fatal(err)
	}
	otherConfig := client.Config()
	otherConfig.Token = "another-job-api-token"
	otherToken := reapi.NewToken()
	removeOther := s.Register(t.Context(), otherToken, &reapi.Backend{API: client.New(otherConfig), BucketURL: bucket})
	defer removeOther()
	otherContext := metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer "+otherToken)
	if _, err := ac.GetActionResult(otherContext, &repb.GetActionResultRequest{InstanceName: "project", ActionDigest: action}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("another job bypassed API authorization: %v", err)
	}
	unregister()
	if _, err := capabilities.GetCapabilities(ctx, &repb.GetCapabilitiesRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expired job: %v", err)
	}
	s.Close()
	_, conn, ctx, _ = connect(t, b)
	ac = repb.NewActionCacheClient(conn)
	got, err := ac.GetActionResult(ctx, &repb.GetActionResultRequest{InstanceName: "project", ActionDigest: action})
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("restored action = %v, %v", got, err)
	}
	if _, err := ac.GetActionResult(ctx, &repb.GetActionResultRequest{InstanceName: "other-project", ActionDigest: action}); status.Code(err) != codes.NotFound {
		t.Fatalf("cross-instance read = %v", err)
	}
	stream, err := bs.NewByteStreamClient(conn).Read(ctx, &bs.ReadRequest{
		ResourceName: fmt.Sprintf("project/blobs/%s/%d", d.Hash, d.SizeBytes),
		ReadOffset:   9,
		ReadLimit:    6,
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := stream.Recv()
	if err != nil || !bytes.Equal(read.Data, payload[9:15]) {
		t.Fatalf("range read = %v, %v", read, err)
	}
	// The metadata can outlive an evicted blob; this must not be an action hit.
	files, _ := filepath.Glob(strings.TrimPrefix(bucket, "file://") + "/*")
	var evicted bool
	for _, file := range files {
		zr, err := zip.OpenReader(file)
		if err != nil {
			continue
		}
		r, err := zr.File[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		_ = zr.Close()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(data, payload) {
			if err := os.WriteFile(file, []byte("corrupt"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ac.GetActionResult(ctx, &repb.GetActionResultRequest{InstanceName: "project", ActionDigest: action}); status.Code(err) != codes.DataLoss {
				t.Fatalf("corrupt output = %v", err)
			}
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			evicted = true
		}
	}
	if !evicted {
		t.Fatal("no output blob was evicted")
	}
	if _, err := ac.GetActionResult(ctx, &repb.GetActionResultRequest{InstanceName: "project", ActionDigest: action}); status.Code(err) != codes.NotFound {
		t.Fatalf("evicted output = %v", err)
	}
}

func TestBackendFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		httpStatus int
		body       string
		want       codes.Code
	}{
		{"missing", 404, `{"message":"Cache entry not found"}`, codes.NotFound},
		{"denied", 404, `{"message":"Cache entry not found","restore_diagnostics":{"attempts":[{"outcome":"denied"}]}}`, codes.PermissionDenied},
		{"incomplete", 404, `{"message":"Cache entry not found","restore_diagnostics":{"search_incomplete":true}}`, codes.Unavailable},
		{"unauthenticated", 401, `{"message":"private-backend-detail"}`, codes.Unauthenticated},
		{"outage", 500, `{"message":"private-backend-detail"}`, codes.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.httpStatus)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			t.Cleanup(srv.Close)
			client := api.NewClient(logger.Discard, api.Config{Endpoint: srv.URL + "/", Token: "job-token"})
			_, conn, ctx, _ := connect(t, &reapi.Backend{API: client})
			_, err := repb.NewActionCacheClient(conn).GetActionResult(ctx, &repb.GetActionResultRequest{ActionDigest: digest([]byte("action"))})
			if status.Code(err) != tc.want || strings.Contains(err.Error(), "private-backend-detail") {
				t.Fatalf("backend failure = %v, want %v without backend detail", err, tc.want)
			}
		})
	}
}

func TestByteStreamWrite(t *testing.T) {
	t.Parallel()
	client, bucket := cacheAPI(t)
	_, conn, ctx, _ := connect(t, &reapi.Backend{API: client, BucketURL: bucket})
	payload := bytes.Repeat([]byte("a binary output\x00"), 150000)
	d := digest(payload)
	clientBS := bs.NewByteStreamClient(conn)
	stream, err := clientBS.Write(ctx)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("uploads/test/blobs/%s/%d", d.Hash, d.SizeBytes)
	for start := 0; start < len(payload); {
		end := min(start+64000, len(payload))
		if err := stream.Send(&bs.WriteRequest{
			ResourceName: name,
			WriteOffset:  int64(start),
			Data:         payload[start:end],
			FinishWrite:  end == len(payload),
		}); err != nil {
			t.Fatal(err)
		}
		start = end
	}
	committed, err := stream.CloseAndRecv()
	if err != nil || committed.CommittedSize != int64(len(payload)) {
		t.Fatalf("write = %v, %v", committed, err)
	}
	read, err := clientBS.Read(ctx, &bs.ReadRequest{ResourceName: fmt.Sprintf("blobs/%s/%d", d.Hash, d.SizeBytes)})
	if err != nil {
		t.Fatal(err)
	}
	var restored []byte
	for {
		part, err := read.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		restored = append(restored, part.Data...)
	}
	if !bytes.Equal(restored, payload) {
		t.Fatal("streamed payload changed")
	}
}

// This is a real Once compatibility check, NOT proof against live Buildkite
// Cache: only its metadata service is replaced by the fixture above.
func TestOnceCompatibility(t *testing.T) {
	bin := os.Getenv("ONCE_TEST_BINARY")
	if bin == "" {
		t.Skip("set ONCE_TEST_BINARY to run the real Once compatibility check")
	}
	client, bucket := cacheAPI(t)
	s, _, _, _ := connect(t, &reapi.Backend{API: client, BucketURL: bucket})
	workspace := t.TempDir()
	config := `[infrastructure.cache]
provider = "tuist"
project = "agent-reapi-test"

[infrastructures.tuist]
kind = "tuist"
account = "buildkite"
url = "http://127.0.0.1:9"
`
	if err := os.WriteFile(filepath.Join(workspace, "once.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
# once input "input.c"
# once output "output"
echo executed >> executions
cc input.c -o output
echo build-stdout
echo build-stderr >&2
`
	if err := os.WriteFile(filepath.Join(workspace, "build.sh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "input.c"), []byte("#include <stdio.h>\nint main(void) { puts(\"restored executable\"); return 0; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var original []byte
	for run := 1; run <= 2; run++ {
		token := reapi.NewToken()
		unregister := s.Register(t.Context(), token, &reapi.Backend{API: client, BucketURL: bucket})
		cmd := exec.CommandContext(t.Context(), bin, "--format", "json", "exec", "--script", "/bin/sh", "build.sh")
		cmd.Dir = workspace
		cmd.Env = append(os.Environ(),
			"TUIST_CACHE_ENDPOINT="+s.Endpoint(),
			"TUIST_TOKEN="+token,
			"XDG_CACHE_HOME="+t.TempDir(),
			"XDG_CONFIG_HOME="+t.TempDir(),
			"XDG_DATA_HOME="+t.TempDir(),
		)
		out, err := cmd.CombinedOutput()
		unregister()
		t.Logf("Once run %d: %s", run, out)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(out, []byte("WARN")) {
			t.Fatal("Once reported a remote operation failure")
		}
		if !bytes.Contains(out, []byte("build-stdout")) || !bytes.Contains(out, []byte("build-stderr")) {
			t.Fatal("stdout or stderr was not replayed")
		}
		if run == 2 && !bytes.Contains(out, []byte(`"cache":"hit"`)) {
			t.Fatal("second invocation did not report a cache hit")
		}
		data, err := os.ReadFile(filepath.Join(workspace, "output"))
		if err != nil {
			t.Fatal(err)
		}
		if run == 1 {
			original = data
			if err := os.Remove(filepath.Join(workspace, "output")); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(filepath.Join(workspace, ".once")); err != nil {
				t.Fatal(err)
			}
		} else if !bytes.Equal(data, original) {
			t.Fatal("restored executable differs")
		}
	}
	executions, err := os.ReadFile(filepath.Join(workspace, "executions"))
	if err != nil || string(executions) != "executed\n" {
		t.Fatalf("command was not remotely reused: %q, %v", executions, err)
	}
	cmd := exec.CommandContext(t.Context(), filepath.Join(workspace, "output"))
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != "restored executable\n" {
		t.Fatalf("restored executable = %q, %v", out, err)
	}
	t.Logf("Verified one execution, replayed streams, and a restored executable: %d bytes, SHA-256 %x", len(original), sha256.Sum256(original))
}
