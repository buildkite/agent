package clicommand

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/require"
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
		require.EqualError(t, err, "at least one secret key must be provided")
	})

	t.Run("error on invalid format", func(t *testing.T) {
		t.Parallel()
		server := newSecretGetTestServer(t, map[string]string{"deploy_key": "shhsecret"})
		defer server.Close()
		var out bytes.Buffer
		err := secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{"deploy_key"}, "xml"), &out, logger.NewBuffer())
		require.EqualError(t, err, `invalid format "xml": must be one of 'default', 'json', or 'env'`)
	})

	t.Run("single key default format prints value", func(t *testing.T) {
		t.Parallel()
		server := newSecretGetTestServer(t, map[string]string{"deploy_key": "shhsecret"})
		defer server.Close()
		var out bytes.Buffer
		err := secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{"deploy_key"}, "default"), &out, logger.NewBuffer())
		require.NoError(t, err)
		require.Equal(t, "shhsecret\n", out.String())
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
		require.NoError(t, err)

		var result map[string]string
		// unmarshalling and requiring the result to ensure the output is valid JSON and has the expected keys and values
		require.NoError(t, json.Unmarshal(out.Bytes(), &result))
		require.Equal(t, map[string]string{
			"deploy_key":     "secret1",
			"github_api_key": "secret2",
		}, result)
	})

	t.Run("json format", func(t *testing.T) {
		t.Parallel()
		server := newSecretGetTestServer(t, map[string]string{"deploy_key": "supersecret"})
		defer server.Close()
		var out bytes.Buffer
		err := secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{"deploy_key"}, "json"), &out, logger.NewBuffer())
		require.NoError(t, err)

		var result map[string]string
		require.NoError(t, json.Unmarshal(out.Bytes(), &result))
		require.Equal(t, map[string]string{"deploy_key": "supersecret"}, result)
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
		require.NoError(t, err)
		require.Equal(t, "DEPLOY_KEY='secret1'\nGITHUB_API_KEY='secret2'\n", out.String())
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
		require.NoError(t, err)
		require.Equal(t, "DEPLOY_KEY=''\\''sec\\nret1'\\'''\nGITHUB_API_KEY='se'\\''c'\\''ret2'\n", out.String())
	})

	t.Run("api error returns error with details", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		var out bytes.Buffer
		err := secretGet(context.Background(), baseSecretGetConfig(server.URL, []string{"missing_key"}, "default"), &out, logger.NewBuffer())
		require.Error(t, err)
		require.Contains(t, err.Error(), "Failed to fetch some secrets:")
		require.Contains(t, err.Error(), "missing_key")
	})
}
