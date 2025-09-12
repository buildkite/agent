package api_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"gotest.tools/v3/assert"
)

func TestGetSecret(t *testing.T) {
	t.Parallel()

	const (
		jobID          = "job-id"
		nonJobID       = "non-job-id"
		secretKey      = "NOT_TEST_SECRET"
		nonSecretKey   = "TEST_SECRET"
		secretValue    = "super-secret"
		secretUUID     = "secret-id"
		accessToken    = "llamas"
		nonAccessToken = "alpacas"
	)

	ctx := context.Background()

	for _, test := range []struct {
		name             string
		accessToken      string
		getSecretRequest *api.GetSecretRequest
		expectedSecret   *api.Secret
		expectedError    error
		expectedCode     int
	}{
		{
			name:        "success",
			accessToken: accessToken,
			getSecretRequest: &api.GetSecretRequest{
				Key:   secretKey,
				JobID: jobID,
			},
			expectedSecret: &api.Secret{
				Key:   secretKey,
				Value: secretValue,
				UUID:  secretUUID,
			},
			expectedError: nil,
			expectedCode:  http.StatusOK,
		},
		{
			name:        "unauthorized",
			accessToken: nonAccessToken,
			getSecretRequest: &api.GetSecretRequest{
				Key:   secretKey,
				JobID: jobID,
			},
			expectedError: errors.New("Unauthorized: got alpacas, want llamas"),
			expectedCode:  http.StatusUnauthorized,
		},
		{
			name:        "job_not_found",
			accessToken: accessToken,
			getSecretRequest: &api.GetSecretRequest{
				Key:   secretKey,
				JobID: nonJobID,
			},
			expectedError: fmt.Errorf("Not Found: method = GET, url = /jobs/%s/secrets?key=%s", nonJobID, secretKey),
			expectedCode:  http.StatusNotFound,
		},
		{
			name:        "secret_not_found",
			accessToken: accessToken,
			getSecretRequest: &api.GetSecretRequest{
				Key:   nonSecretKey,
				JobID: jobID,
			},
			expectedError: fmt.Errorf("Not Found: method = GET, url = /jobs/%s/secrets?key=%s", jobID, nonSecretKey),
			expectedCode:  http.StatusNotFound,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			secretPath := path.Join("/jobs", jobID, "secrets")
			buildkiteAPI := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				if got, want := authToken(req), accessToken; got != want {
					http.Error(
						rw,
						fmt.Sprintf(`{"message": "Unauthorized: got %s, want %s"}`, got, want),
						http.StatusUnauthorized,
					)
					return
				}

				if req.URL.Path == secretPath && req.URL.Query().Get("key") == secretKey {
					_, err := io.WriteString(
						rw, fmt.Sprintf(`{"key":%q,"value":%q,"uuid":%q}`, secretKey, secretValue, secretUUID),
					)
					assert.NilError(t, err)
					return
				}

				http.Error(
					rw,
					fmt.Sprintf(
						`{"message":"Not Found: method = %s, url = %s"}`,
						req.Method,
						req.URL.String(),
					),
					http.StatusNotFound,
				)
			}))
			t.Cleanup(buildkiteAPI.Close)

			// Initial client with a registration token
			client := api.NewClient(logger.Discard, api.Config{
				UserAgent: "Test",
				Endpoint:  buildkiteAPI.URL,
				Token:     test.accessToken,
				DebugHTTP: true,
			})

			secret, resp, err := client.GetSecret(ctx, test.getSecretRequest)
			assert.Check(t, resp.StatusCode == test.expectedCode, "expected status code %d, got %d", test.expectedCode, resp.StatusCode)
			if test.expectedError == nil {
				assert.DeepEqual(t, secret, test.expectedSecret)
			} else if aerr := new(api.ErrorResponse); errors.As(err, &aerr) {
				assert.DeepEqual(t, aerr.Message, test.expectedError.Error())
			} else {
				assert.ErrorIs(t, err, test.expectedError)
			}
		})
	}
}

func TestGetSecrets(t *testing.T) {
	t.Parallel()

	const (
		jobID          = "job-id"
		nonJobID       = "non-job-id"
		secretKey1     = "SECRET_1"
		secretKey2     = "SECRET_2"
		nonSecretKey   = "NON_SECRET"
		secretValue1   = "super-secret-1"
		secretValue2   = "super-secret-2"
		secretUUID1    = "secret-id-1"
		secretUUID2    = "secret-id-2"
		accessToken    = "llamas"
		nonAccessToken = "alpacas"
	)

	ctx := context.Background()

	for _, test := range []struct {
		name                string
		accessToken         string
		getSecretsRequest   *api.GetSecretsRequest
		expectedSecretsResp *api.GetSecretsResponse
		expectedError       error
		expectedCode        int
	}{
		{
			name:        "success_multiple_secrets",
			accessToken: accessToken,
			getSecretsRequest: &api.GetSecretsRequest{
				Keys:  []string{secretKey1, secretKey2},
				JobID: jobID,
			},
			expectedSecretsResp: &api.GetSecretsResponse{
				Secrets: []api.Secret{
					{
						Key:   secretKey1,
						Value: secretValue1,
						UUID:  secretUUID1,
					},
					{
						Key:   secretKey2,
						Value: secretValue2,
						UUID:  secretUUID2,
					},
				},
			},
			expectedError: nil,
			expectedCode:  http.StatusOK,
		},
		{
			name:        "unauthorized",
			accessToken: nonAccessToken,
			getSecretsRequest: &api.GetSecretsRequest{
				Keys:  []string{secretKey1},
				JobID: jobID,
			},
			expectedError: errors.New("Unauthorized: got alpacas, want llamas"),
			expectedCode:  http.StatusUnauthorized,
		},
		{
			name:        "job_not_found",
			accessToken: accessToken,
			getSecretsRequest: &api.GetSecretsRequest{
				Keys:  []string{secretKey1},
				JobID: nonJobID,
			},
			expectedError: fmt.Errorf("Not Found: method = GET, url = /jobs/%s/secrets?key%%5B%%5D=%s", nonJobID, secretKey1),
			expectedCode:  http.StatusNotFound,
		},
		{
			name:        "partial_secrets_not_found",
			accessToken: accessToken,
			getSecretsRequest: &api.GetSecretsRequest{
				Keys:  []string{secretKey1, nonSecretKey},
				JobID: jobID,
			},
			expectedSecretsResp: &api.GetSecretsResponse{
				Secrets: []api.Secret{
					{
						Key:   secretKey1,
						Value: secretValue1,
						UUID:  secretUUID1,
					},
				},
			},
			expectedError: nil,
			expectedCode:  http.StatusOK,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			secretsPath := path.Join("/jobs", jobID, "secrets")
			buildkiteAPI := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				if got, want := authToken(req), accessToken; got != want {
					http.Error(
						rw,
						fmt.Sprintf(`{"message": "Unauthorized: got %s, want %s"}`, got, want),
						http.StatusUnauthorized,
					)
					return
				}

				if req.URL.Path == secretsPath {
					keys := req.URL.Query()["key[]"]
					if len(keys) == 0 {
						// Single key query
						keys = []string{req.URL.Query().Get("key")}
					}

					var secrets []api.Secret
					for _, key := range keys {
						switch key {
						case secretKey1:
							secrets = append(secrets, api.Secret{
								Key:   secretKey1,
								Value: secretValue1,
								UUID:  secretUUID1,
							})
						case secretKey2:
							secrets = append(secrets, api.Secret{
								Key:   secretKey2,
								Value: secretValue2,
								UUID:  secretUUID2,
							})
						}
					}

					if len(secrets) > 0 {
						if len(keys) == 1 && len(secrets) == 1 {
							// Single secret response format for backwards compatibility
							_, err := fmt.Fprintf(rw, `{"key":%q,"value":%q,"uuid":%q}`,
								secrets[0].Key, secrets[0].Value, secrets[0].UUID)
							assert.NilError(t, err)
						} else {
							// Multiple secrets response format
							response := `{"secrets":[`
							for i, secret := range secrets {
								if i > 0 {
									response += ","
								}
								response += fmt.Sprintf(`{"key":%q,"value":%q,"uuid":%q}`,
									secret.Key, secret.Value, secret.UUID)
							}
							response += `]}`
							_, err := io.WriteString(rw, response)
							assert.NilError(t, err)
						}
						return
					}
				}

				http.Error(
					rw,
					fmt.Sprintf(
						`{"message":"Not Found: method = %s, url = %s"}`,
						req.Method,
						req.URL.String(),
					),
					http.StatusNotFound,
				)
			}))
			t.Cleanup(buildkiteAPI.Close)

			// Initial client with a registration token
			client := api.NewClient(logger.Discard, api.Config{
				UserAgent: "Test",
				Endpoint:  buildkiteAPI.URL,
				Token:     test.accessToken,
				DebugHTTP: true,
			})

			secretsResp, resp, err := client.GetSecrets(ctx, test.getSecretsRequest)
			assert.Check(t, resp.StatusCode == test.expectedCode, "expected status code %d, got %d", test.expectedCode, resp.StatusCode)
			if test.expectedError == nil {
				assert.DeepEqual(t, secretsResp, test.expectedSecretsResp)
			} else if aerr := new(api.ErrorResponse); errors.As(err, &aerr) {
				assert.DeepEqual(t, aerr.Message, test.expectedError.Error())
			} else {
				assert.ErrorIs(t, err, test.expectedError)
			}
		})
	}
}
