package job

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/v4/internal/self"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/shellwords"
)

// configureGitCredentialHelper sets up the agent to use a git credential helper that calls the Buildkite Agent API
// asking for a Github App token to use when cloning. This feature is turned on serverside
func (e *Executor) configureGitCredentialHelper(ctx context.Context) error {
	// credential.useHttpPath is a git config setting that tells git to tell the credential helper the full URL of the repo
	// this means that we can pass the repo being cloned up to the BK API, which can then choose (or not, if it's not permitted)
	// to return a token for that repo.
	//
	// This is important for the case where a user clones multiple repos in a step - ie, if we always crammed
	// os.Getenv("BUILDKITE_REPO") into credential helper, we'd only ever get a token for the repo that the step is running
	// in, and not for any other repos that the step might clone.
	err := e.shell.Command("git", "config", "--global", "credential.useHttpPath", "true").Run(ctx, shell.ShowPrompt(false))
	if err != nil {
		return fmt.Errorf("enabling git credential.useHttpPath: %w", err)
	}

	helper := fmt.Sprintf(`%s git-credentials-helper`, self.Path(ctx))
	err = e.shell.Command("git", "config", "--global", "credential.helper", helper).Run(ctx, shell.ShowPrompt(false))
	if err != nil {
		return fmt.Errorf("configuring git credential.helper: %w", err)
	}

	return nil
}

// Disables SSH keyscan and configures git to use HTTPS instead of SSH for github.
// We may later expand this for other SCMs.
func (e *Executor) configureHTTPSInsteadOfSSH(ctx context.Context) error {
	return e.shell.Command(
		"git", "config", "--global", "url.https://github.com/.insteadOf", "git@github.com:",
	).Run(ctx, shell.ShowPrompt(false))
}

// prepareGitSSHKey materialises the configured GitSSHKey into a private
// directory next to the build checkout and points GIT_SSH_COMMAND at it for
// the duration of the checkout phase. It returns the path to the key file, a
// cleanup function that removes the key and restores any previous
// GIT_SSH_COMMAND, and an error. If no key is configured both the path and
// cleanup are zero.
//
// Only the default checkout phase invokes this; custom checkout hooks must
// arrange their own credentials.
func (e *Executor) prepareGitSSHKey() (sshKeyPath string, cleanup func() error, retErr error) {
	if e.GitSSHKey == "" {
		return "", nil, nil
	}

	checkoutPath, exists := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")
	if !exists || checkoutPath == "" {
		return "", nil, errors.New("BUILDKITE_BUILD_CHECKOUT_PATH is not set")
	}

	parentDir := filepath.Dir(checkoutPath)
	if err := os.MkdirAll(parentDir, 0o700); err != nil {
		return "", nil, fmt.Errorf("creating ssh key parent directory %q: %w", parentDir, err)
	}

	pattern := ".buildkite-ssh-key-"
	if e.PipelineSlug != "" {
		pattern += badCharsRE.ReplaceAllString(e.PipelineSlug, "-") + "-"
	}

	// os.MkdirTemp creates the directory with mode 0o700 on Unix, keeping the
	// key file in its own private directory instead of beside the checkout.
	sshKeyDir, err := os.MkdirTemp(parentDir, pattern)
	if err != nil {
		return "", nil, fmt.Errorf("creating ssh key directory: %w", err)
	}
	defer func() {
		if retErr != nil {
			_ = os.RemoveAll(sshKeyDir)
		}
	}()

	sshKeyPath = filepath.Join(sshKeyDir, "id")
	// Most SSH key parsers require a trailing newline; tolerate either form
	// of input and always write a single one.
	keyBytes := []byte(strings.TrimRight(e.GitSSHKey, "\n") + "\n")
	if err := os.WriteFile(sshKeyPath, keyBytes, 0o600); err != nil {
		return "", nil, fmt.Errorf("writing ssh key file: %w", err)
	}

	previousGitSSHCommand, hadPreviousGitSSHCommand := e.shell.Env.Get("GIT_SSH_COMMAND")
	e.shell.Env.Set("GIT_SSH_COMMAND", gitSSHCommandForKeyFile(sshKeyPath, previousGitSSHCommand))

	cleanup = func() error {
		if hadPreviousGitSSHCommand {
			e.shell.Env.Set("GIT_SSH_COMMAND", previousGitSSHCommand)
		} else {
			e.shell.Env.Remove("GIT_SSH_COMMAND")
		}
		if err := os.RemoveAll(sshKeyDir); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	return sshKeyPath, cleanup, nil
}

func gitSSHCommandForKeyFile(path, previous string) string {
	keyOptions := fmt.Sprintf("-i %s -o IdentitiesOnly=yes", shellwords.Quote(path))
	if previous == "" {
		return "ssh " + keyOptions
	}
	return strings.TrimSpace(previous) + " " + keyOptions
}
