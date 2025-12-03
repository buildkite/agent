//go:build e2e

package e2e

import (
	"context"
	"os"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
)

func TestMain(m *testing.M) {
	l := logger.NewConsoleLogger(logger.NewTextPrinter(os.Stderr), os.Exit)
	client := api.NewClient(l, api.Config{
		Token: agentToken,
	})
	ctx := context.Background()
	ident, _, err := client.GetTokenIdentity(ctx)
	if err != nil {
		l.Fatal("Could not read token identity: %v", err)
	}
	targetOrg = ident.OrganizationSlug
	targetCluster = ident.ClusterUUID

	os.Exit(m.Run())
}
