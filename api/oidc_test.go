package api_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-cmp/cmp"
)

func newOIDCTokenServer(
	t *testing.T,
	accessToken, oidcToken, path string,
	expectedBody []byte,
) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case path:
			if got, want := authToken(req), accessToken; got != want {
				http.Error(
					rw,
					fmt.Sprintf("authToken(req) = %q, want %q", got, want),
					http.StatusUnauthorized,
				)
				return
			}

			body, err := io.ReadAll(req.Body)
			if err != nil {
				http.Error(
					rw,
					fmt.Sprintf(`{"message:"Internal Server Error: %q"}`, err),
					http.StatusInternalServerError,
				)
				return
			}

			if !bytes.Equal(body, expectedBody) {
				t.Errorf("wanted = %q, got = %q", expectedBody, body)
				http.Error(
					rw,
					fmt.Sprintf(
						`{"message:"Bad Request: wanted = %q, got = %q"}`,
						expectedBody,
						body,
					),
					http.StatusBadRequest,
				)
				return
			}

			io.WriteString(rw, fmt.Sprintf(`{"token":"%s"}`, oidcToken))

		default:
			http.Error(
				rw,
				fmt.Sprintf(
					`{"message":"Not Found; method = %q, path = %q"}`,
					req.Method,
					req.URL.Path,
				),
				http.StatusNotFound,
			)
		}
	}))
}

func TestOIDCToken(t *testing.T) {
	const jobId = "b078e2d2-86e9-4c12-bf3b-612a8058d0a4"
	const oidcToken = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiYWRtaW4iOnRydWUsImlhdCI6MTUxNjIzOTAyMn0.NHVaYe26MbtOYhSKkoKYdFVomg4i8ZJd8_-RU8VNbftc4TSMb4bXP3l3YlNWACwyXPGffz5aXHc6lty1Y2t4SWRqGteragsVdZufDn5BlnJl9pdR_kdVFUsra2rWKEofkZeIC4yWytE58sMIihvo9H1ScmmVwBcQP6XETqYd0aSHp1gOa9RdUPDvoXQ5oqygTqVtxaDr6wUFKrKItgBMzWIdNZ6y7O9E0DhEPTbE9rfBo6KTFsHAZnMg4k68CDp2woYIaXbmYTWcvbzIuHO7_37GT79XdIwkm95QJ7hYC9RiwrV7mesbY4PAahERJawntho0my942XheVLmGwLMBkQ"
	const accessToken = "llamas"

	for _, testData := range []struct {
		OIDCTokenRequest *api.OIDCTokenRequest
		AccessToken      string
		ExpectedBody     []byte
		OIDCToken        *api.OIDCToken
		Error            error
	}{
		{
			AccessToken: accessToken,
			OIDCTokenRequest: &api.OIDCTokenRequest{
				JobId: jobId,
			},
			ExpectedBody: []byte("{}\n"),
			OIDCToken:    &api.OIDCToken{Token: oidcToken},
		},
		{
			AccessToken: accessToken,
			OIDCTokenRequest: &api.OIDCTokenRequest{
				JobId:    jobId,
				Audience: "sts.amazonaws.com",
			},
			ExpectedBody: []byte(`{"audience":"sts.amazonaws.com"}
`),
			OIDCToken: &api.OIDCToken{Token: oidcToken},
		},
	} {
		func() { // this exists to allow closing the server on each iteration
			path := fmt.Sprintf("/jobs/%s/oidc/tokens", testData.OIDCTokenRequest.JobId)

			server := newOIDCTokenServer(
				t,
				testData.AccessToken,
				testData.OIDCToken.Token,
				path,
				testData.ExpectedBody,
			)
			defer server.Close()

			// Initial client with a registration token
			client := api.NewClient(logger.Discard, api.Config{
				UserAgent: "Test",
				Endpoint:  server.URL,
				Token:     accessToken,
				DebugHTTP: true,
			})

			if token, resp, err := client.OIDCToken(testData.OIDCTokenRequest); err != nil {
				if !errors.Is(err, testData.Error) {
					t.Fatalf(
						"OIDCToken(%v) got error = %v, want error = %v",
						testData.OIDCTokenRequest,
						err,
						testData.Error,
					)
				}
			} else if !cmp.Equal(token, testData.OIDCToken) {
				t.Fatalf("OIDCToken(%v) got token = %v, want %v", testData.OIDCTokenRequest, token, testData.OIDCToken)
			} else if resp.StatusCode != http.StatusOK {
				t.Fatalf("OIDCToken(%v) got StatusCode = %v, want %v", testData.OIDCTokenRequest, resp.StatusCode, http.StatusOK)
			}
		}()
	}
}