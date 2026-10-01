package job

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCheckoutTarget(t *testing.T) {
	t.Parallel()

	const sha = "0123456789abcdef0123456789abcdef01234567"

	tests := []struct {
		name string
		cfg  ExecutorConfig
		want checkoutTarget
	}{
		{
			name: "known commit on a branch",
			cfg:  ExecutorConfig{Commit: sha, Branch: "main", PullRequest: "false"},
			want: checkoutTarget{kind: targetCommit, commit: sha, refspec: "main"},
		},
		{
			name: "HEAD resolves from the branch",
			cfg:  ExecutorConfig{Commit: "HEAD", Branch: "main", PullRequest: "false"},
			want: checkoutTarget{kind: targetBranch, refspec: "main"},
		},
		{
			name: "tag build with a commit and no branch",
			cfg:  ExecutorConfig{Commit: sha, Tag: "v1.0.0", PullRequest: "false"},
			want: checkoutTarget{kind: targetCommit, commit: sha},
		},
		{
			name: "custom refspec wins over everything",
			cfg: ExecutorConfig{
				Commit: "HEAD", Branch: "main", RefSpec: "refs/custom/thing",
				PullRequest: "7", PipelineProvider: "github",
			},
			want: checkoutTarget{kind: targetCustomRefspec, refspec: "refs/custom/thing", pullRequest: "7"},
		},
		{
			name: "GitHub pull request head",
			cfg:  ExecutorConfig{Commit: sha, Branch: "feature", PullRequest: "7", PipelineProvider: "github"},
			want: checkoutTarget{
				kind: targetGithubPRHead, commit: sha, refspec: "refs/pull/7/head", retryFetch: true, pullRequest: "7",
			},
		},
		{
			name: "GitHub Enterprise pull request head with unknown commit",
			cfg:  ExecutorConfig{Commit: "HEAD", Branch: "feature", PullRequest: "7", PipelineProvider: "github_enterprise"},
			want: checkoutTarget{
				kind: targetGithubPRHead, refspec: "refs/pull/7/head", retryFetch: true, pullRequest: "7",
			},
		},
		{
			name: "GitHub pull request merge ref",
			cfg: ExecutorConfig{
				Commit: "HEAD", Branch: "feature", PullRequest: "7", PipelineProvider: "github",
				PullRequestUsingMergeRefspec: true,
			},
			want: checkoutTarget{kind: targetGithubPRMerge, refspec: "refs/pull/7/merge", pullRequest: "7"},
		},
		{
			name: "pull request from another provider is a plain commit",
			cfg:  ExecutorConfig{Commit: sha, Branch: "feature", PullRequest: "7", PipelineProvider: "bitbucket"},
			want: checkoutTarget{kind: targetCommit, commit: sha, refspec: "feature", pullRequest: "7"},
		},
		{
			name: "unset pull request is not a pull request",
			cfg:  ExecutorConfig{Commit: sha, Branch: "main", PipelineProvider: "github"},
			want: checkoutTarget{kind: targetCommit, commit: sha, refspec: "main"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := New(tc.cfg).checkoutTarget()
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(checkoutTarget{})); diff != "" {
				t.Errorf("checkoutTarget() diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCheckoutTargetCheckoutRef(t *testing.T) {
	t.Parallel()

	if got := (checkoutTarget{commit: "abc"}).checkoutRef(); got != "abc" {
		t.Errorf("checkoutRef() with known commit = %q, want %q", got, "abc")
	}
	if got := (checkoutTarget{}).checkoutRef(); got != "FETCH_HEAD" {
		t.Errorf("checkoutRef() with unknown commit = %q, want %q", got, "FETCH_HEAD")
	}
}
