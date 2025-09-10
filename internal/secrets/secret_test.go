package secrets

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
)

func TestFetchSecrets_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/jobs/test-job-id/secrets":
			switch req.URL.Query()["key"][0] {
			case "DATABASE_URL":
				rw.WriteHeader(http.StatusOK)
				fmt.Fprintf(rw, `{"key": "DATABASE_URL", "value": "postgres://user:pass@host:5432/db"}`)
			case "API_TOKEN":
				rw.WriteHeader(http.StatusOK)
				fmt.Fprintf(rw, `{"key": "API_TOKEN", "value": "secret-token-123"}`)
			case "MISSING":
				rw.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(rw, `{"message": "secret not found"}`)
			default:
				rw.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(rw, `{"message": "secret not found"}`)
			}
		default:
			t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	keys := []string{"DATABASE_URL", "API_TOKEN"}
	secrets, errs := FetchSecrets(context.Background(), apiClient, "test-job-id", keys, false)

	if len(errs) > 0 {
		t.Fatalf("expected no errors, got: %v", errs)
	}

	if len(secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(secrets))
	}

	if diff := cmp.Diff(secrets, []Secret{
		{Key: "DATABASE_URL", Value: "postgres://user:pass@host:5432/db"},
		{Key: "API_TOKEN", Value: "secret-token-123"},
	}); diff != "" {
		t.Errorf("unexpected secrets (-want +got):\n%s", diff)
	}
}

func TestFetchSecrets_EmptyKeys(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/jobs/test-job-id/secrets":
			switch req.URL.Query()["key"][0] {
			case "DATABASE_URL":
				rw.WriteHeader(http.StatusOK)
				fmt.Fprintf(rw, `{"key": "DATABASE_URL", "value": "postgres://user:pass@host:5432/db"}`)
			case "API_TOKEN":
				rw.WriteHeader(http.StatusOK)
				fmt.Fprintf(rw, `{"key": "DATABASE_URL", "value": "secret-token-123"}`)
			case "MISSING":
				rw.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(rw, `{"message": "secret not found"}`)
			default:
				rw.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(rw, `{"message": "secret not found"}`)
			}
		default:
			t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	secrets, errs := FetchSecrets(context.Background(), apiClient, "test-job-id", []string{}, false)

	if len(errs) > 0 {
		t.Fatalf("expected no errors, got: %v", errs)
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}
}

func TestFetchSecrets_NilKeys(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/jobs/test-job-id/secrets":
			switch req.URL.Query()["key"][0] {
			case "DATABASE_URL":
				rw.WriteHeader(http.StatusOK)
				fmt.Fprintf(rw, `{"key": "DATABASE_URL", "value": "postgres://user:pass@host:5432/db"}`)
			case "API_TOKEN":
				rw.WriteHeader(http.StatusOK)
				fmt.Fprintf(rw, `{"key": "DATABASE_URL", "value": "secret-token-123"}`)
			case "MISSING":
				rw.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(rw, `{"message": "secret not found"}`)
			default:
				rw.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(rw, `{"message": "secret not found"}`)
			}
		default:
			t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	secrets, errs := FetchSecrets(context.Background(), apiClient, "test-job-id", nil, false)

	if len(errs) > 0 {
		t.Fatalf("expected no errors, got: %v", errs)
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}
}

func TestFetchSecrets_AllOrNothing_SomeSecretsFail(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/jobs/test-job-id/secrets":
			switch req.URL.Query()["key"][0] {
			case "DATABASE_URL":
				rw.WriteHeader(http.StatusOK)
				fmt.Fprintf(rw, `{"key": "DATABASE_URL", "value": "very-secret-value"}`)
			case "API_TOKEN":
				rw.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(rw, `{"message": "secret not found"}`)
			case "MISSING":
				rw.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(rw, `{"message": "secret not found"}`)
			default:
				rw.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(rw, `{"message": "secret not found"}`)
			}
		default:
			t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	keys := []string{"DATABASE_URL", "API_TOKEN", "MISSING"}
	secrets, errs := FetchSecrets(context.Background(), apiClient, "test-job-id", keys, false)

	// Should return errors because some secrets failed
	if len(errs) == 0 {
		t.Fatal("expected errors, got none")
	}

	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}

	var errorStrings []string
	for _, err := range errs {
		errorStrings = append(errorStrings, err.Error())
	}
	allErrors := strings.Join(errorStrings, " ")

	if !strings.Contains(allErrors, `secret "API_TOKEN"`) {
		t.Errorf("expected errors to contain API_TOKEN failure, got: %v", errs)
	}
	if !strings.Contains(allErrors, `secret "MISSING"`) {
		t.Errorf("expected errors to contain MISSING failure, got: %v", errs)
	}
}

func TestFetchSecrets_AllOrNothing_AllSecretsFail(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/jobs/test-job-id/secrets":
			switch req.URL.Query()["key"][0] {
			case "DATABASE_URL":
				rw.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(rw, `{"message": "secret not found"}`)
			case "API_TOKEN":
				rw.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(rw, `{"message": "secret not found"}`)
			default:
				rw.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(rw, `{"message": "secret not found"}`)
			}
		default:
			t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	keys := []string{"API_TOKEN", "DATABASE_URL"}
	secrets, errs := FetchSecrets(context.Background(), apiClient, "test-job-id", keys, false)

	// Should return errors because all secrets failed
	if len(errs) == 0 {
		t.Fatal("expected errors, got none")
	}

	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}
}

func TestFetchSecrets_APIClientError(t *testing.T) {
	t.Parallel()

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This won't be reached
		w.WriteHeader(http.StatusOK)
	}))

	// Start the server manually so we can control the listener
	server.Start()
	defer server.Close()

	// Close the underlying listener to cause connection errors
	server.Listener.Close()

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	keys := []string{"TEST_SECRET"}
	secrets, errs := FetchSecrets(context.Background(), apiClient, "test-job-id", keys, false)

	if len(errs) == 0 {
		t.Fatal("expected errors, got none")
	}

	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}

	var netErr *net.OpError
	if !errors.As(errs[0], &netErr) || !errors.Is(netErr.Err, syscall.ECONNREFUSED) {
		t.Errorf("expected connection refused error, got: %v", errs[0])
	}
}
