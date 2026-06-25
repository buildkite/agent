package jobapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/google/go-cmp/cmp"
)

func pt(s string) *string {
	return &s
}

func testEnviron() *env.Environment {
	e := env.New()
	e.Set("MOUNTAIN", "cotopaxi")
	e.Set("CAPITAL", "quito")

	return e
}

func testEnvironWith(key, value string) *env.Environment {
	e := testEnviron()
	e.Set(key, value)
	return e
}

func testServer(t *testing.T, e *env.Environment, mux *replacer.Mux, opts ...jobapi.ServerOpts) (*jobapi.Server, string, error) {
	sockName, err := jobapi.NewSocketPath(os.TempDir())
	if err != nil {
		return nil, "", fmt.Errorf("creating socket path: %w", err)
	}
	return jobapi.NewServer(shell.TestingLogger{T: t}, sockName, e, mux, opts...)
}

func testSocketClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}
}

func testAPI[Req, Resp any](t *testing.T, env *env.Environment, req *http.Request, client *http.Client, testCase apiTestCase[Req, Resp]) {
	t.Helper()

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected no error for client.Do(req) (got %v)", err)
	}

	if resp.StatusCode != testCase.expectedStatus {
		t.Fatalf("expected status code %d (got %d)", testCase.expectedStatus, resp.StatusCode)
	}

	if testCase.expectedResponseBody != nil {
		var got Resp
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("json.NewDecoder(resp.Body).Decode(&got) error = %v, want nil", err)
		}
		if !cmp.Equal(testCase.expectedResponseBody, &got) {
			t.Fatalf("\n\texpected response: % #v\n\tgot: % #v\n\tdiff = %s)", *testCase.expectedResponseBody, got, cmp.Diff(testCase.expectedResponseBody, &got))
		}
	}

	if testCase.expectedError != nil {
		var got jobapi.ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("json.NewDecoder(resp.Body).Decode(&got) error = %v, want nil", err)
		}
		if got.Error != testCase.expectedError.Error {
			t.Fatalf("expected error %q (got %q)", testCase.expectedError.Error, got.Error)
		}
	}

	if testCase.expectedEnv != nil {
		if !cmp.Equal(testCase.expectedEnv, env.Dump()) {
			t.Fatalf("\n\texpected env: % #v\n\tgot: % #v\n\tdiff = %s)", testCase.expectedEnv, env, cmp.Diff(testCase.expectedEnv, env))
		}
	}
}

func TestServerStartStop(t *testing.T) {
	t.Parallel()

	env := testEnviron()
	srv, _, err := testServer(t, env, replacer.NewMux())
	if err != nil {
		t.Fatalf("testServer(t, env) error = %v", err)
	}

	err = srv.Start()
	if err != nil {
		t.Fatalf("srv.Start() = %v", err)
	}

	// Check the socket path exists and is a socket.
	// Note that os.ModeSocket might not be set on Windows.
	// (https://github.com/golang/go/issues/33357)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(srv.SocketPath)
		if err != nil {
			t.Fatalf("os.Stat(%q) = %v", srv.SocketPath, err)
		}

		if fi.Mode()&os.ModeSocket == 0 {
			t.Fatalf("%q is not a socket", srv.SocketPath)
		}
	}

	// Try to connect to the socket.
	test, err := net.Dial("unix", srv.SocketPath)
	if err != nil {
		t.Fatalf("socket test connection: %v", err)
	}

	if err := test.Close(); err != nil {
		t.Fatalf("test.Close() = %v", err)
	}

	err = srv.Stop()
	if err != nil {
		t.Fatalf("srv.Stop() = %v", err)
	}

	time.Sleep(100 * time.Millisecond) // Wait for the socket file to be unlinked
	_, err = os.Stat(srv.SocketPath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.Stat(%s) = _, os.ErrNotExist, got %v", srv.SocketPath, err)
	}
}

func TestServerStartStop_WithPreExistingSocket(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("socket collision detection isn't support on windows. If the current go version is >1.23, it might be worth re-enabling this test, because hopefully the bug (https://github.com/golang/go/issues/33357) is fixed")
	}

	sockName := filepath.Join(os.TempDir(), "test-socket-collision.sock")
	srv1, _, err := jobapi.NewServer(shell.TestingLogger{T: t}, sockName, env.New(), replacer.NewMux())
	if err != nil {
		t.Fatalf("expected initial server creation to succeed, got %v", err)
	}

	err = srv1.Start()
	if err != nil {
		t.Fatalf("expected initial server start to succeed, got %v", err)
	}
	defer func() {
		if err := srv1.Stop(); err != nil {
			t.Errorf("srv1.Stop() = %v", err)
		}
	}()

	expectedErr := fmt.Sprintf("creating socket server: file already exists at socket path %s", sockName)
	_, _, err = jobapi.NewServer(shell.TestingLogger{T: t}, sockName, env.New(), replacer.NewMux())
	if err == nil {
		t.Fatalf("expected second server creation to fail with %s, got nil", expectedErr)
	}

	if err.Error() != expectedErr {
		t.Fatalf("expected second server start to fail with %q, got %q", expectedErr, err)
	}
}

type apiTestCase[Req, Resp any] struct {
	name                 string
	requestBody          *Req
	expectedStatus       int
	expectedResponseBody *Resp
	expectedEnv          map[string]string
	expectedError        *jobapi.ErrorResponse
}

func TestDeleteEnv(t *testing.T) {
	t.Parallel()

	cases := []apiTestCase[jobapi.EnvDeleteRequest, jobapi.EnvDeleteResponse]{
		{
			name:                 "happy case",
			requestBody:          &jobapi.EnvDeleteRequest{Keys: []string{"MOUNTAIN"}},
			expectedStatus:       http.StatusOK,
			expectedResponseBody: &jobapi.EnvDeleteResponse{Deleted: []string{"MOUNTAIN"}},
			expectedEnv:          env.FromMap(map[string]string{"CAPITAL": "quito"}).Dump(),
		},
		{
			name:                 "deleting a non-existent key is a no-op",
			requestBody:          &jobapi.EnvDeleteRequest{Keys: []string{"NATIONAL_PARKS"}},
			expectedStatus:       http.StatusOK,
			expectedResponseBody: &jobapi.EnvDeleteResponse{Deleted: []string{}},
			expectedEnv:          testEnviron().Dump(), // ie no change
		},
		{
			name: "deleting protected keys returns a 422",
			requestBody: &jobapi.EnvDeleteRequest{
				Keys: []string{"MOUNTAIN", "CAPITAL", "BUILDKITE_AGENT_PID"},
			},
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError: &jobapi.ErrorResponse{
				Error: "the following environment variables are protected, and cannot be modified: [BUILDKITE_AGENT_PID]",
			},
			expectedEnv: testEnviron().Dump(), // ie no change
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			environ := testEnviron()
			srv, token, err := testServer(t, environ, replacer.NewMux())
			if err != nil {
				t.Fatalf("creating server: %v", err)
			}

			err = srv.Start()
			if err != nil {
				t.Fatalf("starting server: %v", err)
			}

			client := testSocketClient(srv.SocketPath)

			defer func() {
				err := srv.Stop()
				if err != nil {
					t.Fatalf("stopping server: %v", err)
				}
			}()

			buf := bytes.NewBuffer(nil)
			err = json.NewEncoder(buf).Encode(c.requestBody)
			if err != nil {
				t.Fatalf("JSON-encoding c.requestBody into buf: %v", err)
			}

			req, err := http.NewRequest(http.MethodDelete, "http://job/api/current-job/v0/env", buf)
			if err != nil {
				t.Fatalf("creating request: %v", err)
			}

			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

			testAPI(t, environ, req, client, c) // Ignore arguments, dial socket
		})
	}
}

func TestPatchEnv(t *testing.T) {
	t.Parallel()

	cases := []apiTestCase[jobapi.EnvUpdateRequestPayload, jobapi.EnvUpdateResponse]{
		{
			name: "happy case",
			requestBody: &jobapi.EnvUpdateRequestPayload{
				Env: map[string]*string{
					"MOUNTAIN":       pt("chimborazo"),
					"CAPITAL":        pt("quito"),
					"NATIONAL_PARKS": pt("cayambe-coca,el-cajas,galápagos"),
				},
			},
			expectedStatus: http.StatusOK,
			expectedResponseBody: &jobapi.EnvUpdateResponse{
				Added:   []string{"NATIONAL_PARKS"},
				Updated: []string{"CAPITAL", "MOUNTAIN"},
			},
			expectedEnv: env.FromMap(map[string]string{
				"MOUNTAIN":       "chimborazo",
				"NATIONAL_PARKS": "cayambe-coca,el-cajas,galápagos",
				"CAPITAL":        "quito",
			}).Dump(),
		},
		{
			name: "setting to nil returns a 422",
			requestBody: &jobapi.EnvUpdateRequestPayload{
				Env: map[string]*string{
					"NATIONAL_PARKS": nil,
					"MOUNTAIN":       pt("chimborazo"),
				},
			},
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError: &jobapi.ErrorResponse{
				Error: "removing environment variables (ie setting them to null) is not permitted on this endpoint. The following keys were set to null: [NATIONAL_PARKS]",
			},
			expectedEnv: testEnviron().Dump(), // ie no changes
		},
		{
			name: "setting protected variables returns a 422",
			requestBody: &jobapi.EnvUpdateRequestPayload{
				Env: map[string]*string{
					"BUILDKITE_AGENT_PID": pt("12345"),
					"MOUNTAIN":            pt("antisana"),
				},
			},
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError: &jobapi.ErrorResponse{
				Error: "the following environment variables are protected, and cannot be modified: [BUILDKITE_AGENT_PID]",
			},
			expectedEnv: testEnviron().Dump(), // ie no changes
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			environ := testEnviron()
			srv, token, err := testServer(t, environ, replacer.NewMux())
			if err != nil {
				t.Fatalf("creating server: %v", err)
			}

			err = srv.Start()
			if err != nil {
				t.Fatalf("starting server: %v", err)
			}

			client := testSocketClient(srv.SocketPath)

			defer func() {
				err := srv.Stop()
				if err != nil {
					t.Fatalf("stopping server: %v", err)
				}
			}()

			buf := bytes.NewBuffer(nil)
			err = json.NewEncoder(buf).Encode(c.requestBody)
			if err != nil {
				t.Fatal(err)
			}

			req, err := http.NewRequest(http.MethodPatch, "http://job/api/current-job/v0/env", buf)
			if err != nil {
				t.Fatalf("creating request: %v", err)
			}

			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

			testAPI(t, environ, req, client, c)
		})
	}
}

func TestPatchEnvAllowsCheckoutScopedVarsWhenNoCheckoutOverrideDisabled(t *testing.T) {
	t.Parallel()

	environ := testEnvironWith("BUILDKITE_GIT_CLONE_FLAGS", "-v")
	srv, token, err := testServer(t, environ, replacer.NewMux())
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("starting server: %v", err)
	}
	defer func() {
		if err := srv.Stop(); err != nil {
			t.Fatalf("stopping server: %v", err)
		}
	}()

	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(&jobapi.EnvUpdateRequestPayload{
		Env: map[string]*string{"BUILDKITE_GIT_CLONE_FLAGS": pt("--mirror")},
	}); err != nil {
		t.Fatalf("encoding request body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPatch, "http://job/api/current-job/v0/env", buf)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	testAPI(t, environ, req, testSocketClient(srv.SocketPath), apiTestCase[jobapi.EnvUpdateRequestPayload, jobapi.EnvUpdateResponse]{
		expectedStatus: http.StatusOK,
		expectedEnv:    testEnvironWith("BUILDKITE_GIT_CLONE_FLAGS", "--mirror").Dump(),
	})
}

func TestPatchEnvRejectsCheckoutScopedVarsWhenNoCheckoutOverrideEnabled(t *testing.T) {
	t.Parallel()

	environ := testEnvironWith("BUILDKITE_SKIP_CHECKOUT", "false")
	srv, token, err := testServer(t, environ, replacer.NewMux(), jobapi.WithNoCheckoutOverride())
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("starting server: %v", err)
	}
	defer func() {
		if err := srv.Stop(); err != nil {
			t.Fatalf("stopping server: %v", err)
		}
	}()

	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(&jobapi.EnvUpdateRequestPayload{
		Env: map[string]*string{"BUILDKITE_SKIP_CHECKOUT": pt("true")},
	}); err != nil {
		t.Fatalf("encoding request body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPatch, "http://job/api/current-job/v0/env", buf)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	testAPI(t, environ, req, testSocketClient(srv.SocketPath), apiTestCase[jobapi.EnvUpdateRequestPayload, jobapi.EnvUpdateResponse]{
		expectedStatus: http.StatusUnprocessableEntity,
		expectedError: &jobapi.ErrorResponse{
			Error: "the following environment variables are protected, and cannot be modified: [BUILDKITE_SKIP_CHECKOUT]. Checkout-related variables are locked because BUILDKITE_NO_CHECKOUT_OVERRIDE is enabled",
		},
		expectedEnv: testEnvironWith("BUILDKITE_SKIP_CHECKOUT", "false").Dump(),
	})
}

func TestDeleteEnvRejectsCheckoutScopedVarsWhenNoCheckoutOverrideEnabled(t *testing.T) {
	t.Parallel()

	environ := testEnvironWith("BUILDKITE_SKIP_CHECKOUT", "false")
	srv, token, err := testServer(t, environ, replacer.NewMux(), jobapi.WithNoCheckoutOverride())
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("starting server: %v", err)
	}
	defer func() {
		if err := srv.Stop(); err != nil {
			t.Fatalf("stopping server: %v", err)
		}
	}()

	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(&jobapi.EnvDeleteRequest{Keys: []string{"BUILDKITE_SKIP_CHECKOUT"}}); err != nil {
		t.Fatalf("encoding request body: %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, "http://job/api/current-job/v0/env", buf)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	testAPI(t, environ, req, testSocketClient(srv.SocketPath), apiTestCase[jobapi.EnvDeleteRequest, jobapi.EnvDeleteResponse]{
		expectedStatus: http.StatusUnprocessableEntity,
		expectedError: &jobapi.ErrorResponse{
			Error: "the following environment variables are protected, and cannot be modified: [BUILDKITE_SKIP_CHECKOUT]. Checkout-related variables are locked because BUILDKITE_NO_CHECKOUT_OVERRIDE is enabled",
		},
		expectedEnv: testEnvironWith("BUILDKITE_SKIP_CHECKOUT", "false").Dump(),
	})
}

func TestPatchEnvRejectsSparseCheckoutPathsWhenNoCheckoutOverrideEnabled(t *testing.T) {
	t.Parallel()

	environ := testEnvironWith("BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS", "a/b")
	srv, token, err := testServer(t, environ, replacer.NewMux(), jobapi.WithNoCheckoutOverride())
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("starting server: %v", err)
	}
	defer func() {
		if err := srv.Stop(); err != nil {
			t.Fatalf("stopping server: %v", err)
		}
	}()

	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(&jobapi.EnvUpdateRequestPayload{
		Env: map[string]*string{"BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS": pt("c/d")},
	}); err != nil {
		t.Fatalf("encoding request body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPatch, "http://job/api/current-job/v0/env", buf)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	testAPI(t, environ, req, testSocketClient(srv.SocketPath), apiTestCase[jobapi.EnvUpdateRequestPayload, jobapi.EnvUpdateResponse]{
		expectedStatus: http.StatusUnprocessableEntity,
		expectedError: &jobapi.ErrorResponse{
			Error: "the following environment variables are protected, and cannot be modified: [BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS]. Checkout-related variables are locked because BUILDKITE_NO_CHECKOUT_OVERRIDE is enabled",
		},
		expectedEnv: testEnvironWith("BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS", "a/b").Dump(),
	})
}

func TestPatchEnvAllowsUnscopedVarsWhenNoCheckoutOverrideEnabled(t *testing.T) {
	t.Parallel()

	// The lock must not over-block: a normal, non-checkout var stays writable
	// while BUILDKITE_NO_CHECKOUT_OVERRIDE is enabled.
	environ := testEnviron()
	srv, token, err := testServer(t, environ, replacer.NewMux(), jobapi.WithNoCheckoutOverride())
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("starting server: %v", err)
	}
	defer func() {
		if err := srv.Stop(); err != nil {
			t.Fatalf("stopping server: %v", err)
		}
	}()

	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(&jobapi.EnvUpdateRequestPayload{
		Env: map[string]*string{"MY_CUSTOM_VAR": pt("hello")},
	}); err != nil {
		t.Fatalf("encoding request body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPatch, "http://job/api/current-job/v0/env", buf)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	testAPI(t, environ, req, testSocketClient(srv.SocketPath), apiTestCase[jobapi.EnvUpdateRequestPayload, jobapi.EnvUpdateResponse]{
		expectedStatus: http.StatusOK,
		expectedEnv:    testEnvironWith("MY_CUSTOM_VAR", "hello").Dump(),
	})
}

func TestGetEnv(t *testing.T) {
	t.Parallel()

	env := testEnviron()
	srv, token, err := testServer(t, env, replacer.NewMux())
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}

	err = srv.Start()
	if err != nil {
		t.Fatalf("starting server: %v", err)
	}

	client := testSocketClient(srv.SocketPath)

	defer func() {
		err := srv.Stop()
		if err != nil {
			t.Fatalf("stopping server: %v", err)
		}
	}()

	req, err := http.NewRequest(http.MethodGet, "http://job/api/current-job/v0/env", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	testAPI(t, env, req, client, apiTestCase[any, jobapi.EnvGetResponse]{
		expectedStatus: http.StatusOK,
		expectedResponseBody: &jobapi.EnvGetResponse{
			Env: testEnviron().Dump(),
		},
	})

	env.Set("MOUNTAIN", "chimborazo")
	env.Set("NATIONAL_PARKS", "cayambe-coca,el-cajas,galápagos")

	expectedEnv := map[string]string{
		"NATIONAL_PARKS": "cayambe-coca,el-cajas,galápagos",
		"MOUNTAIN":       "chimborazo",
		"CAPITAL":        "quito",
	}

	// It responds to out-of-band changes to the environment
	testAPI(t, env, req, client, apiTestCase[any, jobapi.EnvGetResponse]{
		expectedStatus: http.StatusOK,
		expectedResponseBody: &jobapi.EnvGetResponse{
			Env: expectedEnv,
		},
	})
}

func TestCreateRedaction(t *testing.T) {
	t.Parallel()

	const (
		alreadyRedacted = "Guayaquil"
		toRedact        = "Quito"
	)

	writeBuf := &bytes.Buffer{}
	rdc := replacer.New(writeBuf, []string{alreadyRedacted}, redact.Redacted)
	mux := replacer.NewMux(rdc)

	env := testEnviron()
	srv, token, err := testServer(t, env, mux)
	if err != nil {
		t.Fatalf("testServer(t, env, mux) error = %v, want nil", err)
	}

	// write some stuff that won't be redacted
	_, err = rdc.Write([]byte("Go from Guayaquil, until you get to Quito.\n"))
	if err != nil {
		t.Fatalf("rdc.Write([]byte(\"Go from Guayaquil, until you get to Quito.\\n\")) error = %v, want nil", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("srv.Start() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if err := srv.Stop(); err != nil {
			t.Fatalf("srv.Stop() error = %v, want nil", err)
		}
	})

	client := testSocketClient(srv.SocketPath)

	tc := apiTestCase[jobapi.RedactionCreateRequest, jobapi.RedactionCreateResponse]{
		expectedStatus:       http.StatusCreated,
		requestBody:          &jobapi.RedactionCreateRequest{Redact: toRedact},
		expectedResponseBody: &jobapi.RedactionCreateResponse{Redacted: toRedact},
	}

	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(tc.requestBody); err != nil {
		t.Fatalf("json.NewEncoder(buf).Encode(tc.requestBody) error = %v, want nil", err)
	}

	req, err := http.NewRequest(http.MethodPost, "http://job/api/current-job/v0/redactions", buf)
	if err != nil {
		t.Fatalf("http.NewRequest(%q, %q, buf) error = %v, want nil", http.MethodPost, "http://job/api/current-job/v0/redactions", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	testAPI(t, env, req, client, tc)

	// now when we write it, it should be redacted
	_, err = rdc.Write([]byte("From Quito, go back to Guayaquil.\n"))
	if err != nil {
		t.Fatalf("rdc.Write([]byte(\"From Quito, go back to Guayaquil.\\n\")) error = %v, want nil", err)
	}

	if err := mux.Flush(); err != nil {
		t.Fatalf("mux.Flush() = %v", err)
	}

	if got, want := writeBuf.String(), "Go from [REDACTED], until you get to Quito.\nFrom [REDACTED], go back to [REDACTED].\n"; got != want {
		t.Fatalf("writeBuf.String() = %q, want %q", got, want)
	}
}

// startPromiseFailureServer starts a Job API server with the given promised-
// failure declarer and returns a socket client and auth token for it.
func startPromiseFailureServer(t *testing.T, declarer jobapi.PromiseFailureDeclarer) (*http.Client, string) {
	t.Helper()

	srv, token, err := testServer(t, testEnviron(), replacer.NewMux(), jobapi.WithPromiseFailureDeclarer(declarer))
	if err != nil {
		t.Fatalf("testServer() error = %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("srv.Start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Stop(); err != nil {
			t.Errorf("srv.Stop() error = %v", err)
		}
	})
	return testSocketClient(srv.SocketPath), token
}

// promiseFailureRequest POSTs a promise-failure declaration to the Job API and
// returns the HTTP status code and, on success, the response body.
func promiseFailureRequest(t *testing.T, client *http.Client, token string, exitStatus int) (int, *jobapi.PromiseFailureResponse) {
	t.Helper()

	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(&jobapi.PromiseFailureRequest{ExitStatus: exitStatus}); err != nil {
		t.Fatalf("encoding request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, "http://job/api/current-job/v0/promise-failure", buf)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do(req) error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	var body jobapi.PromiseFailureResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return resp.StatusCode, &body
}

func TestPromiseFailure(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		calls []int // exit statuses passed to the declarer
	)
	client, token := startPromiseFailureServer(t, func(_ context.Context, exitStatus int, _ string) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, exitStatus)
		return http.StatusNoContent, nil
	})

	// The first declaration of an exit status calls the Buildkite API and reports
	// "declared"; a repeated call for the same exit status is debounced (no extra
	// API call) and reports "debounced"; a different exit status is declared on
	// its own. All callers see success.
	wantOutcomes := []string{
		jobapi.PromiseFailureDeclared,
		jobapi.PromiseFailureDebounced,
		jobapi.PromiseFailureDeclared,
	}
	for i, exitStatus := range []int{1, 1, 2} {
		got, result := promiseFailureRequest(t, client, token, exitStatus)
		if got != http.StatusOK {
			t.Errorf("promiseFailureRequest(%d) status = %d, want %d", exitStatus, got, http.StatusOK)
		}
		if result.Outcome != wantOutcomes[i] {
			t.Errorf("promiseFailureRequest(%d) outcome = %q, want %q", exitStatus, result.Outcome, wantOutcomes[i])
		}
		if !result.Accepted {
			t.Errorf("promiseFailureRequest(%d) accepted = false, want true", exitStatus)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if diff := cmp.Diff([]int{1, 2}, calls); diff != "" {
		t.Errorf("declarer calls diff (-want +got):\n%s", diff)
	}
}

func TestPromiseFailureInvalidExitStatus(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	client, token := startPromiseFailureServer(t, func(_ context.Context, _ int, _ string) (int, error) {
		calls.Add(1)
		return http.StatusNoContent, nil
	})

	// A non-positive exit status is rejected at the boundary, without reaching
	// the declarer (and therefore the Buildkite API).
	for _, exitStatus := range []int{0, -1} {
		if got, _ := promiseFailureRequest(t, client, token, exitStatus); got != http.StatusBadRequest {
			t.Errorf("promiseFailureRequest(%d) status = %d, want %d", exitStatus, got, http.StatusBadRequest)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("declarer calls = %d, want 0", got)
	}
}

func TestPromiseFailureNotCachedOnTransientError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	client, token := startPromiseFailureServer(t, func(_ context.Context, _ int, _ string) (int, error) {
		calls.Add(1)
		return http.StatusServiceUnavailable, fmt.Errorf("the Buildkite API is unavailable")
	})

	// A transient failure is surfaced in the response body, and isn't cached: a
	// later call for the same exit status retries.
	if got, result := promiseFailureRequest(t, client, token, 1); got != http.StatusOK {
		t.Errorf("first promiseFailureRequest(1) status = %d, want %d", got, http.StatusOK)
	} else if result.Accepted || result.UpstreamStatus != http.StatusServiceUnavailable {
		t.Errorf("first promiseFailureRequest(1) result = %+v, want rejected transient 503", result)
	}
	if got, result := promiseFailureRequest(t, client, token, 1); got != http.StatusOK {
		t.Errorf("second promiseFailureRequest(1) status = %d, want %d", got, http.StatusOK)
	} else if result.Accepted || result.UpstreamStatus != http.StatusServiceUnavailable {
		t.Errorf("second promiseFailureRequest(1) result = %+v, want rejected transient 503", result)
	}

	if got := calls.Load(); got != 2 {
		t.Errorf("declarer calls = %d, want 2 (transient failures must not be cached)", got)
	}
}

func TestPromiseFailureCachedOnTerminalError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	client, token := startPromiseFailureServer(t, func(_ context.Context, _ int, _ string) (int, error) {
		calls.Add(1)
		return http.StatusConflict, fmt.Errorf("a different exit status was already declared")
	})

	// A terminal failure is cached: a later call for the same exit status returns
	// the same result without re-hitting the Buildkite API.
	if got, result := promiseFailureRequest(t, client, token, 1); got != http.StatusOK {
		t.Errorf("first promiseFailureRequest(1) status = %d, want %d", got, http.StatusOK)
	} else if result.Accepted || result.UpstreamStatus != http.StatusConflict || result.Outcome != jobapi.PromiseFailureDeclared {
		t.Errorf("first promiseFailureRequest(1) result = %+v, want declared terminal 409", result)
	}
	if got, result := promiseFailureRequest(t, client, token, 1); got != http.StatusOK {
		t.Errorf("second promiseFailureRequest(1) status = %d, want %d", got, http.StatusOK)
	} else if result.Accepted || result.UpstreamStatus != http.StatusConflict || result.Outcome != jobapi.PromiseFailureDebounced {
		t.Errorf("second promiseFailureRequest(1) result = %+v, want debounced terminal 409", result)
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("declarer calls = %d, want 1 (terminal failures must be cached)", got)
	}
}

func TestPromiseFailureStatusNormalization(t *testing.T) {
	t.Parallel()

	// The declarer reports errors with statuses that aren't usable HTTP error
	// codes: a missing status (network error) and a non-error status.
	client, token := startPromiseFailureServer(t, func(_ context.Context, exitStatus int, _ string) (int, error) {
		switch exitStatus {
		case 1:
			return 0, fmt.Errorf("network error after retries")
		case 2:
			return http.StatusNoContent, fmt.Errorf("error with a non-error status")
		}
		return http.StatusNoContent, nil
	})

	// Both are reported as rejected, so a caller never reads them as accepted.
	for _, exitStatus := range []int{1, 2} {
		if got, result := promiseFailureRequest(t, client, token, exitStatus); got != http.StatusOK {
			t.Errorf("promiseFailureRequest(%d) status = %d, want %d", exitStatus, got, http.StatusOK)
		} else if result.Accepted || result.UpstreamStatus != map[int]int{1: 0, 2: http.StatusNoContent}[exitStatus] {
			t.Errorf("promiseFailureRequest(%d) result = %+v, want rejected transient", exitStatus, result)
		}
	}
}

func TestPromiseFailurePanicConcurrent(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	var panicking atomic.Bool
	panicking.Store(true)
	client, token := startPromiseFailureServer(t, func(_ context.Context, _ int, _ string) (int, error) {
		calls.Add(1)
		if panicking.Load() {
			time.Sleep(20 * time.Millisecond)
			panic("declarer boom")
		}
		return http.StatusNoContent, nil
	})

	// A burst of callers coalesce onto a leader whose declaration panics. The
	// leader is recovered to a rejected result and every waiter must also unblock,
	// rather than hanging on a channel that's never closed. (If any caller hung,
	// the wait below would never return and the test would time out.)
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			if got, result := promiseFailureRequest(t, client, token, 1); got != http.StatusOK {
				t.Errorf("promiseFailureRequest(1) status = %d, want %d", got, http.StatusOK)
			} else if result.Accepted || result.UpstreamStatus != http.StatusInternalServerError {
				t.Errorf("promiseFailureRequest(1) result = %+v, want rejected panic result", result)
			}
		}()
	}
	wg.Wait()

	if got := calls.Load(); got < 1 {
		t.Errorf("declarer calls = %d, want at least 1", got)
	}

	// The panic dropped the entry rather than caching a poisoned result, so a
	// later call gets a fresh attempt and succeeds.
	panicking.Store(false)
	before := calls.Load()
	if got, _ := promiseFailureRequest(t, client, token, 1); got != http.StatusOK {
		t.Errorf("post-panic promiseFailureRequest(1) status = %d, want %d", got, http.StatusOK)
	}
	if got := calls.Load(); got != before+1 {
		t.Errorf("declarer calls = %d, want %d (a fresh attempt after panic)", got, before+1)
	}
}

func TestPromiseFailureConcurrent(t *testing.T) {
	t.Parallel()

	// The declarer sleeps briefly so concurrent callers pile up behind the first
	// one and exercise the coalescing wait path.
	var calls atomic.Int64
	client, token := startPromiseFailureServer(t, func(_ context.Context, _ int, _ string) (int, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return http.StatusNoContent, nil
	})

	// Many processes declaring the same exit status concurrently must result in
	// exactly one declaration to the Buildkite API, while every caller still
	// sees the successful outcome.
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			if got, _ := promiseFailureRequest(t, client, token, 1); got != http.StatusOK {
				t.Errorf("promiseFailureRequest(1) status = %d, want %d", got, http.StatusOK)
			}
		}()
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("declarer calls = %d, want exactly 1", got)
	}
}

func TestDebugLogging(t *testing.T) {
	t.Parallel()

	env := testEnviron()

	sockName, err := jobapi.NewSocketPath(os.TempDir())
	if err != nil {
		t.Fatalf("jobapi.NewSocketPath(%q) error = %v, want nil", os.TempDir(), err)
	}

	logBuf := &bytes.Buffer{}
	logger := shell.NewWriterLogger(logBuf, true, nil)
	srv, token, err := jobapi.NewServer(logger, sockName, env, nil, jobapi.WithDebug())
	if err != nil {
		t.Fatalf("jobapi.NewServer(logger, %q, env, %v, jobapi.WithDebug()) error = %v, want nil", sockName, nil, err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("srv.Start() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = srv.Stop() }) // ignore error that server is already stopped

	client := testSocketClient(srv.SocketPath)

	tc := apiTestCase[any, jobapi.EnvGetResponse]{
		expectedStatus: http.StatusOK,
		expectedResponseBody: &jobapi.EnvGetResponse{
			Env: map[string]string{
				"CAPITAL":  "quito",
				"MOUNTAIN": "cotopaxi",
			},
		},
	}

	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(tc.requestBody); err != nil {
		t.Fatalf("json.NewEncoder(buf).Encode(tc.requestBody) error = %v, want nil", err)
	}

	req, err := http.NewRequest(http.MethodGet, "http://job/api/current-job/v0/env", buf)
	if err != nil {
		t.Fatalf("http.NewRequest(%q, %q, buf) error = %v, want nil", http.MethodGet, "http://job/api/current-job/v0/env", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	testAPI(t, env, req, client, tc)

	if err := srv.Stop(); err != nil {
		t.Fatalf("srv.Stop() error = %v, want nil", err)
	}

	logs := logBuf.String()
	if got := strings.Contains(logs, "~~~ Job API"); !got {
		t.Errorf("logs: %q", logs)
	}
	if got := strings.Contains(logs, "Server listening on"); !got {
		t.Errorf("logs: %q", logs)
	}
	if got := strings.Contains(logs, "/api/current-job/v0/env"); !got {
		t.Errorf("logs: %q", logs)
	}
	if got := strings.Contains(logs, "Successfully shut down Job API server"); !got {
		t.Errorf("logs: %q", logs)
	}
}

func TestNoLogging(t *testing.T) {
	t.Parallel()

	env := testEnviron()

	sockName, err := jobapi.NewSocketPath(os.TempDir())
	if err != nil {
		t.Fatalf("jobapi.NewSocketPath(%q) error = %v, want nil", os.TempDir(), err)
	}

	logBuf := &bytes.Buffer{}
	logger := shell.NewWriterLogger(logBuf, true, nil)
	srv, token, err := jobapi.NewServer(logger, sockName, env, nil)
	if err != nil {
		t.Fatalf("jobapi.NewServer(logger, %q, env, %v) error = %v, want nil", sockName, nil, err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("srv.Start() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = srv.Stop() }) // ignore error that server is already stopped

	client := testSocketClient(srv.SocketPath)

	tc := apiTestCase[any, jobapi.EnvGetResponse]{
		expectedStatus: http.StatusOK,
		expectedResponseBody: &jobapi.EnvGetResponse{
			Env: map[string]string{
				"CAPITAL":  "quito",
				"MOUNTAIN": "cotopaxi",
			},
		},
	}

	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(tc.requestBody); err != nil {
		t.Fatalf("json.NewEncoder(buf).Encode(tc.requestBody) error = %v, want nil", err)
	}

	req, err := http.NewRequest(http.MethodGet, "http://job/api/current-job/v0/env", buf)
	if err != nil {
		t.Fatalf("http.NewRequest(%q, %q, buf) error = %v, want nil", http.MethodGet, "http://job/api/current-job/v0/env", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	testAPI(t, env, req, client, tc)

	if err := srv.Stop(); err != nil {
		t.Fatalf("srv.Stop() error = %v, want nil", err)
	}

	logs := logBuf.String()
	if got := logs == ""; !got {
		t.Errorf("logs: %q", logs)
	}
}
