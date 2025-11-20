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
	"testing"
	"time"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/google/go-cmp/cmp"
	"gotest.tools/v3/assert"
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

func testServer(t *testing.T, e *env.Environment, mux *replacer.Mux) (*jobapi.Server, string, error) {
	sockName, err := jobapi.NewSocketPath(os.TempDir())
	if err != nil {
		return nil, "", fmt.Errorf("creating socket path: %w", err)
	}
	return jobapi.NewServer(shell.TestingLogger{T: t}, sockName, e, mux)
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
		assert.NilError(t, json.NewDecoder(resp.Body).Decode(&got))
		if !cmp.Equal(testCase.expectedResponseBody, &got) {
			t.Fatalf("\n\texpected response: % #v\n\tgot: % #v\n\tdiff = %s)", *testCase.expectedResponseBody, got, cmp.Diff(testCase.expectedResponseBody, &got))
		}
	}

	if testCase.expectedError != nil {
		var got jobapi.ErrorResponse
		assert.NilError(t, json.NewDecoder(resp.Body).Decode(&got))
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

	test.Close()

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
	defer srv1.Stop()

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
	assert.NilError(t, err)

	// write some stuff that won't be redacted
	_, err = rdc.Write([]byte("Go from Guayaquil, until you get to Quito.\n"))
	assert.NilError(t, err)

	assert.NilError(t, srv.Start())
	t.Cleanup(func() {
		assert.NilError(t, srv.Stop())
	})

	client := testSocketClient(srv.SocketPath)

	tc := apiTestCase[jobapi.RedactionCreateRequest, jobapi.RedactionCreateResponse]{
		expectedStatus:       http.StatusCreated,
		requestBody:          &jobapi.RedactionCreateRequest{Redact: toRedact},
		expectedResponseBody: &jobapi.RedactionCreateResponse{Redacted: toRedact},
	}

	buf := &bytes.Buffer{}
	assert.NilError(t, json.NewEncoder(buf).Encode(tc.requestBody))

	req, err := http.NewRequest(http.MethodPost, "http://job/api/current-job/v0/redactions", buf)
	assert.NilError(t, err)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	testAPI(t, env, req, client, tc)

	// now when we write it, it should be redacted
	_, err = rdc.Write([]byte("From Quito, go back to Guayaquil.\n"))
	assert.NilError(t, err)

	mux.Flush()

	assert.Equal(
		t,
		writeBuf.String(),
		"Go from [REDACTED], until you get to Quito.\nFrom [REDACTED], go back to [REDACTED].\n",
	)
}

func TestDebugLogging(t *testing.T) {
	t.Parallel()

	env := testEnviron()

	sockName, err := jobapi.NewSocketPath(os.TempDir())
	assert.NilError(t, err)

	logBuf := &bytes.Buffer{}
	logger := shell.NewWriterLogger(logBuf, true, nil)
	srv, token, err := jobapi.NewServer(logger, sockName, env, nil, jobapi.WithDebug())
	assert.NilError(t, err)

	assert.NilError(t, srv.Start())
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
	assert.NilError(t, json.NewEncoder(buf).Encode(tc.requestBody))

	req, err := http.NewRequest(http.MethodGet, "http://job/api/current-job/v0/env", buf)
	assert.NilError(t, err)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	testAPI(t, env, req, client, tc)

	assert.NilError(t, srv.Stop())

	logs := logBuf.String()
	assert.Check(t, strings.Contains(logs, "~~~ Job API"), "logs: %q", logs)
	assert.Check(t, strings.Contains(logs, "Server listening on"), "logs: %q", logs)
	assert.Check(t, strings.Contains(logs, "/api/current-job/v0/env"), "logs: %q", logs)
	assert.Check(t, strings.Contains(logs, "Successfully shut down Job API server"), "logs: %q", logs)
}

func TestNoLogging(t *testing.T) {
	t.Parallel()

	env := testEnviron()

	sockName, err := jobapi.NewSocketPath(os.TempDir())
	assert.NilError(t, err)

	logBuf := &bytes.Buffer{}
	logger := shell.NewWriterLogger(logBuf, true, nil)
	srv, token, err := jobapi.NewServer(logger, sockName, env, nil)
	assert.NilError(t, err)

	assert.NilError(t, srv.Start())
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
	assert.NilError(t, json.NewEncoder(buf).Encode(tc.requestBody))

	req, err := http.NewRequest(http.MethodGet, "http://job/api/current-job/v0/env", buf)
	assert.NilError(t, err)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	testAPI(t, env, req, client, tc)

	assert.NilError(t, srv.Stop())

	logs := logBuf.String()
	assert.Assert(t, logs == "", "logs: %q", logs)
}
