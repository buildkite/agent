// package bkgql contains a client to the Buildkite GraphQL API
package bkgql

//go:generate go run github.com/Khan/genqlient

import (
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/buildkite/agent/v3/internal/agenthttp"
)

const (
	DefaultEndpoint = "https://graphql.buildkite.com/v1"
	graphQLTimeout  = 60 * time.Second
)

func NewClient(endpoint, token string) graphql.Client {
	return graphql.NewClient(endpoint, agenthttp.NewClient(
		agenthttp.WithAuthBearer(token),
		agenthttp.WithTimeout(graphQLTimeout),
	))
}
