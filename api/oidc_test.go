package api_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-cmp/cmp"
)

type testOIDCTokenServer struct {
	accessToken    string
	oidcToken      string
	jobID          string
	forbiddenJobID string
	expectedBody   []byte
}

func (s *testOIDCTokenServer) New(t *testing.T) *httptest.Server {
	t.Helper()
	path := fmt.Sprintf("/jobs/%s/oidc/tokens", url.PathEscape(s.jobID))
	forbiddenPath := fmt.Sprintf("/jobs/%s/oidc/tokens", url.PathEscape(s.forbiddenJobID))
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if got, want := authToken(req), s.accessToken; got != want {
			http.Error(
				rw,
				fmt.Sprintf("authToken(req) = %q, want %q", got, want),
				http.StatusUnauthorized,
			)
			return
		}

		switch req.URL.Path {
		case path:
			body, err := io.ReadAll(req.Body)
			if err != nil {
				http.Error(
					rw,
					fmt.Sprintf(`{"message:"Internal Server Error: %s"}`, err),
					http.StatusInternalServerError,
				)
				return
			}

			if !bytes.Equal(body, s.expectedBody) {
				t.Errorf("wanted = %q, got = %q", s.expectedBody, body)
				http.Error(
					rw,
					fmt.Sprintf(`{"message:"Bad Request: wanted = %q, got = %q"}`, s.expectedBody, body),
					http.StatusBadRequest,
				)
				return
			}

			fmt.Fprintf(rw, `{"token":"%s"}`, s.oidcToken) //nolint:errcheck // The test would still fail

		case forbiddenPath:
			http.Error(
				rw,
				fmt.Sprintf(`{"message":"Forbidden; method = %q, path = %q"}`, req.Method, req.URL.Path),
				http.StatusForbidden,
			)

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
	const jobID = "b078e2d2-86e9-4c12-bf3b-612a8058d0a4"
	const unauthorizedJobID = "a078e2d2-86e9-4c12-bf3b-612a8058d0a4"
	const oidcToken = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiYWRtaW4iOnRydWUsImlhdCI6MTUxNjIzOTAyMn0.NHVaYe26MbtOYhSKkoKYdFVomg4i8ZJd8_-RU8VNbftc4TSMb4bXP3l3YlNWACwyXPGffz5aXHc6lty1Y2t4SWRqGteragsVdZufDn5BlnJl9pdR_kdVFUsra2rWKEofkZeIC4yWytE58sMIihvo9H1ScmmVwBcQP6XETqYd0aSHp1gOa9RdUPDvoXQ5oqygTqVtxaDr6wUFKrKItgBMzWIdNZ6y7O9E0DhEPTbE9rfBo6KTFsHAZnMg4k68CDp2woYIaXbmYTWcvbzIuHO7_37GT79XdIwkm95QJ7hYC9RiwrV7mesbY4PAahERJawntho0my942XheVLmGwLMBkQ"
	const accessToken = "llamas"
	const audience = "sts.amazonaws.com"
	const lifetime = 600

	ctx := context.Background()

	tests := []struct {
		OIDCTokenRequest *api.OIDCTokenRequest
		AccessToken      string
		ExpectedBody     []byte
		OIDCToken        *api.OIDCToken
	}{
		{
			AccessToken: accessToken,
			OIDCTokenRequest: &api.OIDCTokenRequest{
				Job: jobID,
			},
			ExpectedBody: []byte("{}\n"),
			OIDCToken:    &api.OIDCToken{Token: oidcToken},
		},
		{
			AccessToken: accessToken,
			OIDCTokenRequest: &api.OIDCTokenRequest{
				Job:      jobID,
				Audience: audience,
			},
			ExpectedBody: fmt.Appendf(nil, `{"audience":%q}`+"\n", audience),
			OIDCToken:    &api.OIDCToken{Token: oidcToken},
		},
		{
			AccessToken: accessToken,
			OIDCTokenRequest: &api.OIDCTokenRequest{
				Job:      jobID,
				Lifetime: lifetime,
			},
			ExpectedBody: fmt.Appendf(nil, `{"lifetime":%d}`+"\n", lifetime),
			OIDCToken:    &api.OIDCToken{Token: oidcToken},
		},
		{
			AccessToken: accessToken,
			OIDCTokenRequest: &api.OIDCTokenRequest{
				Job:    jobID,
				Claims: []string{"organization_id", "pipeline_id"},
			},
			ExpectedBody: []byte(`{"claims":["organization_id","pipeline_id"]}` + "\n"),
			OIDCToken:    &api.OIDCToken{Token: oidcToken},
		},
		{
			AccessToken: accessToken,
			OIDCTokenRequest: &api.OIDCTokenRequest{
				Job:            jobID,
				AWSSessionTags: []string{"organization_id", "pipeline_id"},
			},
			ExpectedBody: []byte(`{"aws_session_tags":["organization_id","pipeline_id"]}` + "\n"),
			OIDCToken:    &api.OIDCToken{Token: oidcToken},
		},
	}

	for _, test := range tests {
		func() { // this exists to allow closing the server on each iteration
			server := (&testOIDCTokenServer{
				accessToken:    test.AccessToken,
				oidcToken:      test.OIDCToken.Token,
				jobID:          jobID,
				forbiddenJobID: unauthorizedJobID,
				expectedBody:   test.ExpectedBody,
			}).New(t)
			defer server.Close()

			// Initial client with a registration token
			client := api.NewClient(logger.Discard, api.Config{
				UserAgent: "Test",
				Endpoint:  server.URL,
				Token:     accessToken,
				DebugHTTP: true,
			})

			token, resp, err := client.OIDCToken(ctx, test.OIDCTokenRequest)
			if err != nil {
				t.Errorf(
					"OIDCToken(%v) got error = %v",
					test.OIDCTokenRequest,
					err,
				)
				return
			}

			if !cmp.Equal(token, test.OIDCToken) {
				t.Errorf(
					"OIDCToken(%v) got token = %v, want %v",
					test.OIDCTokenRequest,
					token,
					test.OIDCToken,
				)
			}

			if resp.StatusCode != http.StatusOK {
				t.Errorf(
					"OIDCToken(%v) got StatusCode = %v, want %v",
					test.OIDCTokenRequest,
					resp.StatusCode,
					http.StatusOK,
				)
			}
		}()
	}
}

func TestOIDCTokenError(t *testing.T) {
	const jobID = "b078e2d2-86e9-4c12-bf3b-612a8058d0a4"
	const unauthorizedJobID = "a078e2d2-86e9-4c12-bf3b-612a8058d0a4"
	const accessToken = "llamas"
	const audience = "sts.amazonaws.com"

	ctx := context.Background()

	tests := []struct {
		OIDCTokenRequest *api.OIDCTokenRequest
		AccessToken      string
		ExpectedStatus   int
		// TODO: make api.ErrorReponse a serializable type and populate this field
		// ExpectedErr error
	}{
		{
			AccessToken: "camels",
			OIDCTokenRequest: &api.OIDCTokenRequest{
				Job:      jobID,
				Audience: audience,
			},
			ExpectedStatus: http.StatusUnauthorized,
		},
		{
			AccessToken: accessToken,
			OIDCTokenRequest: &api.OIDCTokenRequest{
				Job:      unauthorizedJobID,
				Audience: audience,
			},
			ExpectedStatus: http.StatusForbidden,
		},
		{
			AccessToken: accessToken,
			OIDCTokenRequest: &api.OIDCTokenRequest{
				Job:      "2",
				Audience: audience,
			},
			ExpectedStatus: http.StatusNotFound,
		},
	}

	for _, test := range tests {
		func() { // this exists to allow closing the server on each iteration
			server := (&testOIDCTokenServer{
				accessToken:    test.AccessToken,
				jobID:          jobID,
				forbiddenJobID: unauthorizedJobID,
			}).New(t)
			defer server.Close()

			// Initial client with a registration token
			client := api.NewClient(logger.Discard, api.Config{
				UserAgent: "Test",
				Endpoint:  server.URL,
				Token:     accessToken,
				DebugHTTP: true,
			})

			_, resp, err := client.OIDCToken(ctx, test.OIDCTokenRequest)
			// TODO: make api.ErrorReponse a serializable type and test that the right error type is returned here
			if err == nil {
				t.Errorf("OIDCToken(%v) did not return an error as expected", test.OIDCTokenRequest)
			}

			if resp.StatusCode != test.ExpectedStatus {
				t.Errorf(
					"OIDCToken(%v) got StatusCode = %v, want %v",
					test.OIDCTokenRequest,
					resp.StatusCode,
					test.ExpectedStatus,
				)
			}
		}()
	}
}
