package cache

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
)

func TestRestoreReport(t *testing.T) {
	t.Parallel()
	feature := map[string]string{"branch": "feat-123", "pipeline": "ci"}
	main := map[string]string{"branch": "main", "pipeline": "ci"}
	for _, test := range []struct {
		name   string
		result RestoreResult
		err    error
		want   string
	}{
		{
			name: "illustrative layout",
			result: RestoreResult{
				CacheRestored: true, FallbackUsed: true, Key: "npm-v1", Scopes: main,
				Diagnostics: &api.CacheRestoreDiagnostics{Attempts: []api.CacheRestoreAttempt{
					{CacheKey: []string{"npm", "v1", "arm64", "lock123"}, Scopes: feature, Outcome: "miss"},
					{CacheKey: []string{"npm", "v1", "arm64"}, Scopes: feature, Outcome: "miss"},
					{CacheKey: []string{"npm", "v1", "arm64"}, Scopes: main, Outcome: "denied", Rule: "forks-read-only"},
					{CacheKey: []string{"npm", "v1"}, Scopes: main, Outcome: "hit"},
				}},
			},
			want: "Restoring cache: npm\n" +
				"  npm-v1-arm64-lock123 · pipeline=ci, branch=feat-123 → miss\n" +
				"  npm-v1-arm64         · pipeline=ci, branch=feat-123 → miss\n" +
				"  npm-v1-arm64         · pipeline=ci, branch=main → denied (rule: forks-read-only)\n" +
				"  npm-v1               · pipeline=ci, branch=main → hit\n" +
				"Cache restored using fallback key npm-v1 from pipeline=ci, branch=main",
		},
		{
			name: "exact hit selects actual entry scopes",
			result: RestoreResult{
				CacheRestored: true, CacheHit: true, Key: "npm-v2", Scopes: main,
				Diagnostics: &api.CacheRestoreDiagnostics{Attempts: []api.CacheRestoreAttempt{
					{CacheKey: []string{"npm", "v2"}, Scopes: map[string]string{"pipeline": "ci"}, Outcome: "hit"},
				}},
			},
			want: "Restoring cache: npm\n  npm-v2 · pipeline=ci → hit\nCache restored using exact key npm-v2 from pipeline=ci, branch=main",
		},
		{
			name: "unconstrained search selects a scoped entry",
			result: RestoreResult{
				CacheRestored: true, Key: "npm", Scopes: main,
				Diagnostics: &api.CacheRestoreDiagnostics{Attempts: []api.CacheRestoreAttempt{
					{CacheKey: []string{"npm"}, Outcome: "hit"},
				}},
			},
			want: "Restoring cache: npm\n  npm · any scope → hit\nCache restored using exact key npm from pipeline=ci, branch=main",
		},
		{
			name: "complete miss at budget threshold",
			result: RestoreResult{Diagnostics: &api.CacheRestoreDiagnostics{BudgetExhausted: true, Attempts: []api.CacheRestoreAttempt{
				{CacheKey: []string{"npm"}, Outcome: "miss"},
			}}},
			want: "Restoring cache: npm\n  npm · any scope → miss\n  Registry search budget exhausted\nCache miss",
		},
		{
			name: "budget skips later candidates after a definite miss",
			result: RestoreResult{Diagnostics: &api.CacheRestoreDiagnostics{BudgetExhausted: true, SearchIncomplete: true, Attempts: []api.CacheRestoreAttempt{
				{CacheKey: []string{"npm"}, Outcome: "miss"},
			}}},
			want: "Restoring cache: npm\n  npm · any scope → miss\n  Registry search budget exhausted\nCache not restored: search incomplete",
		},
		{
			name: "default denial is not a miss",
			result: RestoreResult{Diagnostics: &api.CacheRestoreDiagnostics{Attempts: []api.CacheRestoreAttempt{
				{CacheKey: []string{"npm"}, Outcome: "denied"},
			}}},
			want: "Restoring cache: npm\n  npm · any scope → denied (no matching allow rule)\nCache not restored: no allowed entry found (registry policy denied matching entries)",
		},
		{
			name: "ordinary miss",
			result: RestoreResult{Diagnostics: &api.CacheRestoreDiagnostics{Attempts: []api.CacheRestoreAttempt{
				{CacheKey: []string{"npm"}, Outcome: "miss"},
			}}},
			want: "Restoring cache: npm\n  npm · any scope → miss\nCache miss",
		},
		{
			name: "budget exhaustion is not a miss",
			result: RestoreResult{Diagnostics: &api.CacheRestoreDiagnostics{BudgetExhausted: true, Attempts: []api.CacheRestoreAttempt{
				{CacheKey: []string{"npm"}, Outcome: "incomplete"},
			}}},
			want: "Restoring cache: npm\n  npm · any scope → incomplete\n  Registry search budget exhausted\nCache not restored: search incomplete",
		},
		{
			name: "budget exhausted before any lookup",
			result: RestoreResult{Diagnostics: &api.CacheRestoreDiagnostics{
				CacheKey: []string{"npm", "v1"}, ScopeCandidates: []map[string]string{feature, main, {}}, BudgetExhausted: true, SearchIncomplete: true,
			}},
			want: "Restoring cache: npm\n  Key: npm-v1\n  pipeline=ci, branch=feat-123 → not searched\n  pipeline=ci, branch=main → not searched\n  any scope → not searched\n  Registry search budget exhausted\nCache not restored: search incomplete",
		},
		{
			name: "best effort hit is still restored",
			result: RestoreResult{CacheRestored: true, FallbackUsed: true, Key: "npm-old", Diagnostics: &api.CacheRestoreDiagnostics{
				BudgetExhausted: true, SearchIncomplete: true, Attempts: []api.CacheRestoreAttempt{{CacheKey: []string{"npm"}, Outcome: "hit"}},
			}},
			want: "Restoring cache: npm\n  npm · any scope → hit\n  Registry search budget exhausted\nCache restored using fallback key npm-old from unscoped",
		},
		{
			name:   "older server miss is unknown",
			result: RestoreResult{Key: "npm-v1"},
			want:   "Restoring cache: npm\n  Key: npm-v1 (search diagnostics unavailable)\nCache not restored (search diagnostics unavailable)",
		},
		{
			name: "download failure preserves registry hit",
			result: RestoreResult{CacheHit: true, Diagnostics: &api.CacheRestoreDiagnostics{Attempts: []api.CacheRestoreAttempt{
				{CacheKey: []string{"npm"}, Outcome: "hit"},
			}}},
			err:  errors.New("download failed"),
			want: "Restoring cache: npm\n  npm · any scope → hit\nFailed to restore cache: download failed",
		},
		{
			name: "unusable archive is not a successful restore",
			result: RestoreResult{
				NotRestoredReason: "Cache miss (missing blob, invalidated stale entry)",
				Diagnostics: &api.CacheRestoreDiagnostics{Attempts: []api.CacheRestoreAttempt{
					{CacheKey: []string{"npm"}, Outcome: "hit"},
				}},
			},
			want: "Restoring cache: npm\n  npm · any scope → hit\nCache miss (missing blob, invalidated stale entry)",
		},
		{
			name: "escape values without injecting log lines",
			result: RestoreResult{Diagnostics: &api.CacheRestoreDiagnostics{Attempts: []api.CacheRestoreAttempt{
				{CacheKey: []string{"npm\n--- injected"}, Scopes: map[string]string{"branch": "main\r\x1b[K"}, Outcome: "denied", Rule: "rule\n+++ injected"},
			}}},
			want: "Restoring cache: npm\n  npm\\n--- injected · branch=main\\r\\x1b[K → denied (rule: rule\\n+++ injected)\nCache not restored: no allowed entry found (registry policy denied matching entries)",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.want, restoreReport("npm", test.result, test.err)); diff != "" {
				t.Errorf("report (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRestoreWithClient_ConcurrentReports(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var started atomic.Int32
	ready := make(chan struct{})
	client := &mockCacheClient{restoreFunc: func(ctx context.Context, cacheID string) (RestoreResult, error) {
		// Both workers must be in Restore before either can finish.
		if started.Add(1) == 2 {
			close(ready)
		}
		select {
		case <-ctx.Done():
			return RestoreResult{}, ctx.Err()
		case <-ready:
		}
		return RestoreResult{Diagnostics: &api.CacheRestoreDiagnostics{Attempts: []api.CacheRestoreAttempt{
			{CacheKey: []string{cacheID, "key"}, Outcome: "denied", Rule: "#2"},
		}}}, nil
	}}
	log := logger.NewBuffer()
	if err := restoreWithClient(ctx, log, client, []string{"npm", "go"}, 2, false); err != nil {
		t.Fatal(err)
	}
	if len(log.Messages) != 2 {
		t.Fatalf("got %d log messages, want one per cache: %v", len(log.Messages), log.Messages)
	}
	for _, name := range []string{"npm", "go"} {
		want := "[info] Restoring cache: " + name + "\n  " + name + "-key · any scope → denied (rule: #2)\nCache not restored: no allowed entry found (registry policy denied matching entries)"
		if !strings.Contains(strings.Join(log.Messages, "\n"), want) {
			t.Errorf("missing intact report %q in %v", want, log.Messages)
		}
	}
}
