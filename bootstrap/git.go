package bootstrap

import (
	"strings"

	"github.com/buildkite/agent/bootstrap/shell"
	shellwords "github.com/mattn/go-shellwords"
)

func gitClone(sh *shell.Shell, gitCloneFlags, repository, dir string) error {
	individualCloneFlags, err := shellwords.Parse(gitCloneFlags)
	if err != nil {
		return err
	}

	commandArgs := []string{"clone"}
	commandArgs = append(commandArgs, individualCloneFlags...)
	commandArgs = append(commandArgs, "--", repository, ".")

	if err = sh.Run("git", commandArgs...); err != nil {
		return err
	}

	return nil
}

func gitClean(sh *shell.Shell, gitCleanFlags string, gitSubmodules bool) error {
	individualCleanFlags, err := shellwords.Parse(gitCleanFlags)
	if err != nil {
		return err
	}

	commandArgs := []string{"clean"}
	commandArgs = append(commandArgs, individualCleanFlags...)

	if err = sh.Run("git", commandArgs...); err != nil {
		return err
	}

	// Also clean up submodules if we can
	if gitSubmodules {
		commandArgs = append([]string{"submodule", "foreach", "--recursive"}, commandArgs...)

		if err = sh.Run("git", commandArgs...); err != nil {
			return err
		}
	}

	return nil
}

func gitFetch(sh *shell.Shell, gitFetchFlags, repository string, refSpec ...string) error {
	individualFetchFlags, err := shellwords.Parse(gitFetchFlags)
	if err != nil {
		return err
	}

	commandArgs := []string{"fetch"}
	commandArgs = append(commandArgs, individualFetchFlags...)
	commandArgs = append(commandArgs, repository)

	for _, r := range refSpec {
		individualRefSpecs, err := shellwords.Parse(r)
		if err != nil {
			return err
		}
		commandArgs = append(commandArgs, individualRefSpecs...)
	}

	if err = sh.Run("git", commandArgs...); err != nil {
		return err
	}

	return nil
}

func gitEnumerateSubmoduleURLs(sh *shell.Shell) ([]string, error) {
	urls := []string{}

	// The output of this command looks like:
	// Entering 'vendor/docs'
	// git@github.com:buildkite/docs.git
	// Entering 'vendor/frontend'
	// git@github.com:buildkite/frontend.git
	// Entering 'vendor/frontend/vendor/emojis'
	// git@github.com:buildkite/emojis.git
	output, err := sh.RunAndCapture(
		"git", "submodule", "foreach", "--recursive", "git", "remote get-url", "origin")
	if err != nil {
		return nil, err
	}

	// splits into "Entering" "'vendor/blah'" "git@github.com:blah/.."
	// this should work for windows and unix line endings
	for idx, val := range strings.Fields(output) {
		// every third element to get the git@github.com:blah bit
		if idx%3 == 2 {
			urls = append(urls, val)
		}
	}

	return urls, nil
}
