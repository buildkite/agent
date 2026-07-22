package job

import (
	"context"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v4/internal/self"
)

const CommitMetadataKey = "buildkite:git:commit"

// sendCommitToBuildkite sends commit information (commit, author, subject, body) to Buildkite, as the BK backend doesn't
// have access to user's VCSes. To do this, we set a special meta-data key in the build, but only if it isn't already present
// Functionally, this means that the first job in a build (usually a pipeline upload or similar) will push the commit info
// to buildkite, which uses this info to display commit info in the UI eg in the title for the build
// note that we bail early if the key already exists, as we don't want to overwrite it
func (e *Executor) sendCommitToBuildkite(ctx context.Context) error {
	if e.SkipCheckout {
		return nil
	}

	e.shell.Commentf("Checking to see if git commit information needs to be sent to Buildkite...")

	commitResolved, _ := e.shell.Env.Get("BUILDKITE_COMMIT_RESOLVED")
	if commitResolved == "true" {
		// we can skip the metadata shenanigans here and push straight through
		e.shell.Commentf("BUILDKITE_COMMIT is already resolved and meta-data populated, skipping")
		return nil
	}

	cmd := e.shell.Command(self.Path(ctx), "meta-data", "exists", CommitMetadataKey)
	if err := cmd.Run(ctx); err == nil {
		// Command exited 0, ie the key exists, so we don't need to send it again
		e.shell.Commentf("Git commit information has already been sent to Buildkite")
		return nil
	}

	e.shell.Commentf("Sending Git commit information back to Buildkite")
	// Format:
	//
	// commit 0123456789abcdef0123456789abcdef01234567
	// abbrev-commit 0123456789
	// Author: John Citizen <john@example.com>
	//
	//    Subject of the commit message
	//
	//    Body of the commit message, which
	//    may span multiple lines.
	gitArgs := []string{
		"--no-pager",
		"log",
		"-1",
		e.Commit,
		"-s", // --no-patch was introduced in v1.8.4 in 2013, but e.g. CentOS 7 isn't there yet
		"--no-color",
		"--format=commit %H%nabbrev-commit %h%nAuthor: %an <%ae>%n%n%w(0,4,4)%B",
	}
	out, err := e.shell.Command("git", gitArgs...).RunAndCaptureStdout(ctx)
	if err != nil {
		return fmt.Errorf("getting git commit information: %w", err)
	}

	stdin := strings.NewReader(out)
	cmd = e.shell.CloneWithStdin(stdin).Command(self.Path(ctx), "meta-data", "set", CommitMetadataKey)
	if err := cmd.Run(ctx); err != nil {
		return fmt.Errorf("sending git commit information to Buildkite: %w", err)
	}

	return nil
}

func (e *Executor) resolveCommit(ctx context.Context) {
	commitRef, _ := e.shell.Env.Get("BUILDKITE_COMMIT")
	if commitRef == "" {
		e.shell.Warningf("BUILDKITE_COMMIT was empty")
		return
	}
	cmdOut, err := e.shell.Command("git", "rev-parse", commitRef).RunAndCaptureStdout(ctx)
	if err != nil {
		e.shell.Warningf("Error running git rev-parse %q: %v", commitRef, err)
		return
	}
	trimmedCmdOut := strings.TrimSpace(string(cmdOut))
	if trimmedCmdOut != commitRef {
		e.shell.Commentf("Updating BUILDKITE_COMMIT from %q to %q", commitRef, trimmedCmdOut)
		e.shell.Env.Set("BUILDKITE_COMMIT", trimmedCmdOut)
	}
}
