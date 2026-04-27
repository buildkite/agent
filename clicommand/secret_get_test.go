package clicommand

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-cmp/cmp"
)

func newSecretGetTestServer(t *testing.T, secrets map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		key := req.URL.Query().Get("key")
		val, ok := secrets[key]
		if !ok {
			_, _ = fmt.Fprintf(rw, `{"message": "secret not found"}`)
			return
		}
		_, _ = fmt.Fprintf(rw, `{"key": %q, "value": %q, "uuid": "test-uuid"}`, key, val)
	}))
}

func baseSecretGetConfig(serverURL string, keys []string, format string) SecretGetConfig {
	return SecretGetConfig{
		Keys:   keys,
		Format: format,
		Job:    "job-123",
		APIConfig: APIConfig{
			AgentAccessToken: "agentaccesstoken",
			Endpoint:         serverURL,
		},
		SkipRedaction: true,
	}
}

func TestSecretGet(t *testing.T) {
	t.Run("error when no keys provided", func(t *testing.T) {
		t.Parallel()
		server := newSecretGetTestServer(t, map[string]string{})
		defer server.Close()
		var out bytes.Buffer
		err := secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{}, "default"), &out, logger.NewBuffer())
		if want := "at least one secret key must be provided"; err == nil || err.Error() != want {
			t.Fatalf("secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{}, \"default\"), &out, logger.NewBuffer()) error = %v, want error with message %q", err, want)
		}
	})

	t.Run("error on invalid format", func(t *testing.T) {
		t.Parallel()
		server := newSecretGetTestServer(t, map[string]string{"deploy_key": "shhsecret"})
		defer server.Close()
		var out bytes.Buffer
		err := secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{"deploy_key"}, "xml"), &out, logger.NewBuffer())
		if want := `invalid format "xml": must be one of 'default', 'json', or 'env'`; err == nil || err.Error() != want {
			t.Fatalf("secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{\"deploy_key\"}, \"xml\"), &out, logger.NewBuffer()) error = %v, want error with message %q", err, want)
		}
	})

	t.Run("single key default format prints value", func(t *testing.T) {
		t.Parallel()
		server := newSecretGetTestServer(t, map[string]string{"deploy_key": "shhsecret"})
		defer server.Close()
		var out bytes.Buffer
		err := secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{"deploy_key"}, "default"), &out, logger.NewBuffer())
		if err != nil {
			t.Fatalf("secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{\"deploy_key\"}, \"default\"), &out, logger.NewBuffer()) error = %v, want nil", err)
		}
		if got, want := out.String(), "shhsecret\n"; got != want {
			t.Fatalf("out.String() = %q, want %q", got, want)
		}
	})

	t.Run("multiple keys default format outputs json", func(t *testing.T) {
		t.Parallel()
		server := newSecretGetTestServer(t, map[string]string{
			"deploy_key":     "secret1",
			"github_api_key": "secret2",
		})
		defer server.Close()
		var out bytes.Buffer
		err := secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{"deploy_key", "github_api_key"}, "default"), &out, logger.NewBuffer())
		if err != nil {
			t.Fatalf("secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{\"deploy_key\", \"github_api_key\"}, \"default\"), &out, logger.NewBuffer()) error = %v, want nil", err)
		}

		var result map[string]string
		// unmarshalling and requiring the result to ensure the output is valid JSON and has the expected keys and values
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("json.Unmarshal(out.Bytes(), &result) error = %v, want nil", err)
		}
		if diff := cmp.Diff(result, map[string]string{
			"deploy_key":     "secret1",
			"github_api_key": "secret2",
		}); diff != "" {
			t.Fatalf("result diff (-got +want):\n%s", diff)
		}
	})

	t.Run("json format", func(t *testing.T) {
		t.Parallel()
		server := newSecretGetTestServer(t, map[string]string{"deploy_key": "supersecret"})
		defer server.Close()
		var out bytes.Buffer
		err := secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{"deploy_key"}, "json"), &out, logger.NewBuffer())
		if err != nil {
			t.Fatalf("secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{\"deploy_key\"}, \"json\"), &out, logger.NewBuffer()) error = %v, want nil", err)
		}

		var result map[string]string
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("json.Unmarshal(out.Bytes(), &result) error = %v, want nil", err)
		}
		if diff := cmp.Diff(result, map[string]string{"deploy_key": "supersecret"}); diff != "" {
			t.Fatalf("result diff (-got +want):\n%s", diff)
		}
	})

	t.Run("env format - sorts keys, uppercases and shellQuotes", func(t *testing.T) {
		t.Parallel()
		server := newSecretGetTestServer(t, map[string]string{
			"deploy_key":     "secret1",
			"github_api_key": "secret2",
		})
		defer server.Close()

		var out bytes.Buffer
		err := secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{"deploy_key", "github_api_key"}, "env"), &out, logger.NewBuffer())
		if err != nil {
			t.Fatalf("secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{\"deploy_key\", \"github_api_key\"}, \"env\"), &out, logger.NewBuffer()) error = %v, want nil", err)
		}
		if got, want := out.String(), "DEPLOY_KEY='secret1'\nGITHUB_API_KEY='secret2'\n"; got != want {
			t.Fatalf("out.String() = %q, want %q", got, want)
		}
	})

	t.Run("env format - secret contains newline and single quote, returns valid shell syntax", func(t *testing.T) {
		t.Parallel()
		server := newSecretGetTestServer(t, map[string]string{
			"deploy_key":     "'sec\\nret1'",
			"github_api_key": "se'c'ret2",
		})
		defer server.Close()

		var out bytes.Buffer
		err := secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{"deploy_key", "github_api_key"}, "env"), &out, logger.NewBuffer())
		if err != nil {
			t.Fatalf("secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{\"deploy_key\", \"github_api_key\"}, \"env\"), &out, logger.NewBuffer()) error = %v, want nil", err)
		}
		if got, want := out.String(), "DEPLOY_KEY=''\\''sec\\nret1'\\'''\nGITHUB_API_KEY='se'\\''c'\\''ret2'\n"; got != want {
			t.Fatalf("out.String() = %q, want %q", got, want)
		}
	})

	t.Run("api error returns error with details", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		var out bytes.Buffer
		err := secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{"missing_key"}, "default"), &out, logger.NewBuffer())
		if err == nil {
			t.Fatalf("secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{\"missing_key\"}, \"default\"), &out, logger.NewBuffer()) error = %v, want non-nil error", err)
		}
		if got, want := err.Error(), "Failed to fetch some secrets:"; !strings.Contains(got, want) {
			t.Fatalf("err.Error() = %q, want containing %q", got, want)
		}
		if got, want := err.Error(), "missing_key"; !strings.Contains(got, want) {
			t.Fatalf("err.Error() = %q, want containing %q", got, want)
		}
	})
}
