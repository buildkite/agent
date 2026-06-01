package api

import (
	"context"
	"fmt"
	"net/http"
)

type GithubCodeAccessTokenRequest struct {
	RepoURL string `json:"repo_url,omitempty"`
}

type GithubCodeAccessTokenResponse struct {
	Token string `json:"token,omitempty"`
}

func (c *Client) GenerateGithubCodeAccessToken(ctx context.Context, repoURL, jobID string) (string, *Response, error) {
	u := fmt.Sprintf("jobs/%s/github_code_access_token", railsPathEscape(jobID))

	req, err := c.newRequest(ctx, http.MethodPost, u, GithubCodeAccessTokenRequest{RepoURL: repoURL})
	if err != nil {
		return "", nil, err
	}

	var g GithubCodeAccessTokenResponse
	resp, err := c.doRequest(req, &g)
	if err != nil {
		return "", resp, err
	}
	return g.Token, resp, nil
}
