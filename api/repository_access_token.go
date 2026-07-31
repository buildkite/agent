package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/buildkite/roko"
)

type RepositoryAccessTokenRequest struct {
	RepoURL string `json:"repo_url,omitempty"`
}

type RepositoryAccessTokenResponse struct {
	Token string `json:"token,omitempty"`
}

// Deprecated: use RepositoryAccessTokenRequest.
type GithubCodeAccessTokenRequest = RepositoryAccessTokenRequest

// Deprecated: use RepositoryAccessTokenResponse.
type GithubCodeAccessTokenResponse = RepositoryAccessTokenResponse

func (c *Client) GenerateRepositoryAccessToken(ctx context.Context, repoURL, jobID string) (string, *Response, error) {
	u := fmt.Sprintf("jobs/%s/repository_access_token", railsPathEscape(jobID))

	req, err := c.newRequest(ctx, http.MethodPost, u, RepositoryAccessTokenRequest{RepoURL: repoURL})
	if err != nil {
		return "", nil, err
	}

	r := roko.NewRetrier(
		roko.WithMaxAttempts(3),
		roko.WithStrategy(roko.Constant(5*time.Second)),
	)

	var tokenResponse RepositoryAccessTokenResponse

	resp, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) (*Response, error) {
		resp, err := c.doRequest(req, &tokenResponse)
		if err == nil {
			return resp, nil
		}

		if resp != nil {
			if !IsRetryableStatus(resp) {
				r.Break()
				return resp, err
			}

			if resp.Header.Get("Retry-After") != "" {
				retryAfter, errParseDuration := time.ParseDuration(resp.Header.Get("Retry-After") + "s")
				if errParseDuration == nil {
					r.SetNextInterval(retryAfter)
				}
			}
		}

		return resp, err
	})
	if err != nil {
		return "", resp, err
	}

	return tokenResponse.Token, resp, nil
}

// GenerateGithubCodeAccessToken is retained for compatibility.
// Deprecated: use GenerateRepositoryAccessToken.
func (c *Client) GenerateGithubCodeAccessToken(ctx context.Context, repoURL, jobID string) (string, *Response, error) {
	return c.GenerateRepositoryAccessToken(ctx, repoURL, jobID)
}
