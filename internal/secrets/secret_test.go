package secrets

import (
	"context"

	"github.com/google/go-cmp/cmp"

	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	keys := []string{"DATABASE_URL", "API_TOKEN"}
	secrets, err := FetchSecrets(context.Background(), apiClient, "test-job-id", keys, false)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
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

	secrets, err := FetchSecrets(context.Background(), apiClient, "test-job-id", []string{}, false)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
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

	secrets, err := FetchSecrets(context.Background(), apiClient, "test-job-id", nil, false)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
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
	secrets, err := FetchSecrets(context.Background(), apiClient, "test-job-id", keys, false)

	// Should return error because some secrets failed
	if err == nil {
		t.Fatal("expected error, got none")
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}

	// Error should contain details of all failed secrets
	if !strings.Contains(err.Error(), `secret "API_TOKEN": secret not found`) {
		t.Errorf("expected error to contain API_TOKEN failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), `secret "MISSING": secret not found`) {
		t.Errorf("expected error to contain MISSING failure, got: %v", err)
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
	secrets, err := FetchSecrets(context.Background(), apiClient, "test-job-id", keys, false)

	// Should return error because all secrets failed
	if err == nil {
		t.Fatal("expected error, got none")
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
	secrets, err := FetchSecrets(context.Background(), apiClient, "test-job-id", keys, false)

	if err == nil {
		t.Fatal("expected error, got none")
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}

	if !strings.Contains(err.Error(), `connection refused`) {
		t.Errorf("expected error to contain network error, got: %v", err)
	}
}
