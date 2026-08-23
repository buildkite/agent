//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/buildkite/agent/v4/api"
)

func TestMain(m *testing.M) {
	l := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := api.NewClient(l, api.Config{
		Token: agentToken,
	})
	// TestMain has no testing.T, so this setup call intentionally uses a root context.
	ctx := context.Background()
	ident, _, err := client.GetTokenIdentity(ctx)
	if err != nil {
		l.Error(fmt.Sprintf("Could not read token identity: %v", err))
		os.Exit(1)
	}
	targetOrg = ident.OrganizationSlug
	targetCluster = ident.ClusterUUID

	os.Exit(m.Run())
}
