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

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
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
					if err != nil {
						t.Fatalf("io.WriteString(rw, %q) error = %v, want nil", fmt.Sprintf(`{"key":%q,"value":%q,"uuid":%q}`, secretKey, secretValue, secretUUID), err)
					}
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
			client := api.NewClient(logger.SlogDiscard, api.Config{
				UserAgent: "Test",
				Endpoint:  buildkiteAPI.URL,
				Token:     test.accessToken,
				DebugHTTP: true,
			})

			secret, resp, err := client.GetSecret(ctx, test.getSecretRequest)
			if got := resp.StatusCode == test.expectedCode; !got {
				t.Errorf("expected status code %d, got %d", test.expectedCode, resp.StatusCode)
			}
			if test.expectedError == nil {
				if diff := cmp.Diff(test.expectedSecret, secret); diff != "" {
					t.Fatalf("test.expectedSecret diff (-got +want):\n%s", diff)
				}
			} else if aerr := new(api.ErrorResponse); errors.As(err, &aerr) {
				if diff := cmp.Diff(test.expectedError.Error(), aerr.Message); diff != "" {
					t.Fatalf("test.expectedError.Error() diff (-got +want):\n%s", diff)
				}
			} else {
				if want := test.expectedError; !errors.Is(err, want) {
					t.Fatalf("client.GetSecret(ctx, test.getSecretRequest) error = %v, want %v", err, want)
				}
			}
		})
	}
}
