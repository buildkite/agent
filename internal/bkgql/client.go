// package bkgql contains a client to the Buildkite GraphQL API
package bkgql

//go:generate go run github.com/Khan/genqlient

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Khan/genqlient/graphql"
)

const (
	graphQLEndpoint = "https://graphql.buildkite.com/v1"
	graphQLTimeout  = 60 * time.Second
)

func NewClient(token string) graphql.Client {
	return graphql.NewClient("https://graphql.buildkite.com/v1", &http.Client{
		Timeout:   graphQLTimeout,
		Transport: &authedTransport{token: token, wrapped: http.DefaultTransport},
	})
}

type authedTransport struct {
	token   string
	wrapped http.RoundTripper
}

func (t *authedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", t.token))
	return t.wrapped.RoundTrip(req)
}
