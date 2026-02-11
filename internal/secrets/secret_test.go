package secrets

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
)

var noSleep = WithRetrySleepFunc(func(time.Duration) {})

func TestFetchSecrets_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/jobs/test-job-id/secrets":
			switch req.URL.Query()["key"][0] {
			case "DATABASE_URL":
				rw.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprintf(rw, `{"key": "DATABASE_URL", "value": "postgres://user:pass@host:5432/db"}`)
			case "API_TOKEN":
				rw.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprintf(rw, `{"key": "API_TOKEN", "value": "secret-token-123"}`)
			}
		default:
			t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	secrets, errs := FetchSecrets(t.Context(), logger.Discard, apiClient, "test-job-id", []string{"DATABASE_URL", "API_TOKEN"}, 10)
	if len(errs) > 0 {
		t.Fatalf("expected no errors, got: %v", errs)
	}

	if len(secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(secrets))
	}

	secretSorter := func(i, j Secret) bool { return i.Key < j.Key }

	if diff := cmp.Diff(secrets, []Secret{
		{Key: "DATABASE_URL", Value: "postgres://user:pass@host:5432/db"},
		{Key: "API_TOKEN", Value: "secret-token-123"},
	}, cmpopts.SortSlices(secretSorter)); diff != "" {
		t.Errorf("unexpected secrets (-want +got):\n%s", diff)
	}
}

func TestFetchSecrets_EmptyKeys(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		t.Errorf("expected no requests to be made, but got %s %s", req.Method, req.URL.Path)
		http.Error(rw, "Not found", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	apiClient := api.NewClient(logger.Discard, api.Config{Endpoint: server.URL, Token: "llamas"})
	secrets, errs := FetchSecrets(t.Context(), logger.Discard, apiClient, "test-job-id", []string{}, 10)

	if len(errs) > 0 {
		t.Fatalf("expected no errors, got: %v", errs)
	}

	if len(secrets) > 0 {
		t.Errorf("expected empty secrets, got: %v", secrets)
	}
}

func TestFetchSecrets_NilKeys(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		t.Errorf("expected no requests to be made, but got %s %s", req.Method, req.URL.Path)
		http.Error(rw, "Not found", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	apiClient := api.NewClient(logger.Discard, api.Config{Endpoint: server.URL, Token: "llamas"})

	secrets, errs := FetchSecrets(t.Context(), logger.Discard, apiClient, "test-job-id", nil, 10)

	if len(errs) > 0 {
		t.Fatalf("expected no errors, got: %v", errs)
	}

	if len(secrets) > 0 {
		t.Errorf("expected empty secrets, got: %v", secrets)
	}
}

func TestFetchSecrets_SomeSecretsFail(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/jobs/test-job-id/secrets":
			switch req.URL.Query()["key"][0] {
			case "DATABASE_URL":
				rw.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprintf(rw, `{"key": "DATABASE_URL", "value": "very-secret-value"}`)
			case "MISSING":
				rw.WriteHeader(http.StatusNotFound)
				_, _ = fmt.Fprintf(rw, `{"message": "secret not found"}`)
			}
		default:
			t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	keys := []string{"DATABASE_URL", "MISSING"}
	secrets, errs := FetchSecrets(t.Context(), logger.Discard, apiClient, "test-job-id", keys, 10)

	if len(errs) != 1 {
		t.Fatalf("expected 1 errors, got %d: %v", len(errs), errs)
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}

	// Check that all errors are SecretError instances
	for _, err := range errs {
		var secretErr *SecretError
		if !errors.As(err, &secretErr) {
			t.Errorf("expected SecretError, got: %T", err)
		}
	}

	var errorStrings []string
	for _, err := range errs {
		errorStrings = append(errorStrings, err.Error())
	}
	allErrors := strings.Join(errorStrings, " ")

	if !strings.Contains(allErrors, `secret "MISSING"`) {
		t.Errorf("expected errors to contain MISSING failure, got: %v", errs)
	}
}

func TestFetchSecrets_AllSecretsFail(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(rw, `{"message": "secret not found"}`)
	}))
	t.Cleanup(server.Close)

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	keys := []string{"API_TOKEN", "DATABASE_URL"}
	secrets, errs := FetchSecrets(t.Context(), logger.Discard, apiClient, "test-job-id", keys, 10)

	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}

	// Check that all errors are SecretError instances
	for _, err := range errs {
		var secretErr *SecretError
		if !errors.As(err, &secretErr) {
			t.Errorf("expected SecretError, got: %T", err)
		}
	}
}

func TestFetchSecrets_APIClientError(t *testing.T) {
	t.Parallel()

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This won't be reached
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	// Start the server manually so we can control the listener
	server.Start()
	defer server.Close()

	// Close the underlying listener to cause connection errors
	_ = server.Listener.Close()

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	keys := []string{"TEST_SECRET"}
	secrets, errs := FetchSecrets(t.Context(), logger.Discard, apiClient, "test-job-id", keys, 10, noSleep)

	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}

	var secretErr *SecretError
	if !errors.As(errs[0], &secretErr) {
		t.Errorf("expected SecretError, got: %T", errs[0])
	}

	if secretErr.Key != "TEST_SECRET" {
		t.Errorf("expected SecretError key to be 'TEST_SECRET', got: %q", secretErr.Key)
	}

	var netErr *net.OpError
	if !errors.As(secretErr.Err, &netErr) {
		t.Errorf("expected underlying network error, got: %v", secretErr.Err)
		return
	}

	// Check for connection refused errors across platforms
	isConnRefused := false
	if runtime.GOOS == "windows" {
		// On Windows, check for WSAECONNREFUSED (10061)
		if errno, ok := netErr.Err.(syscall.Errno); ok && errno == 10061 {
			isConnRefused = true
		}
	} else {
		// On Unix systems, use syscall.ECONNREFUSED
		isConnRefused = errors.Is(netErr.Err, syscall.ECONNREFUSED)
	}

	// Fallback to string matching if the syscall check didn't work
	if !isConnRefused {
		errStr := netErr.Err.Error()
		isConnRefused = strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "actively refused")
	}

	if !isConnRefused {
		t.Errorf("expected connection refused error, got: %v (type: %T)", netErr.Err, netErr.Err)
	}
}

func TestFetchSecrets_RetriesOnServerError(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			// First two attempts return 502 Bad Gateway (retryable)
			rw.WriteHeader(http.StatusBadGateway)
			_, _ = fmt.Fprintf(rw, `{"message": "bad gateway"}`)
			return
		}
		// Third attempt succeeds
		rw.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(rw, `{"key": "MY_SECRET", "value": "secret-value"}`)
	}))
	t.Cleanup(server.Close)

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	secrets, errs := FetchSecrets(t.Context(), logger.Discard, apiClient, "test-job-id", []string{"MY_SECRET"}, 10, noSleep)
	if len(errs) > 0 {
		t.Fatalf("expected no errors after retries, got: %v", errs)
	}

	if len(secrets) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(secrets))
	}

	if secrets[0].Value != "secret-value" {
		t.Errorf("expected secret value %q, got %q", "secret-value", secrets[0].Value)
	}

	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts (2 failures + 1 success), got %d", got)
	}
}

func TestFetchSecrets_NoRetryOnNonRetryableStatus(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		attempts.Add(1)
		// 404 is not retryable
		rw.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(rw, `{"message": "secret not found"}`)
	}))
	t.Cleanup(server.Close)

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	secrets, errs := FetchSecrets(t.Context(), logger.Discard, apiClient, "test-job-id", []string{"MISSING"}, 10, noSleep)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}

	// Should have only attempted once since 404 is not retryable
	if got := attempts.Load(); got != 1 {
		t.Errorf("expected 1 attempt (no retries for 404), got %d", got)
	}
}
