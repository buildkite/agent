package bootstrap

import (
	"strings"

	"github.com/buildkite/agent/bootstrap/shell"
)

func gitClean(sh *shell.Shell, gitCleanFlags string, gitSubmodules bool) error {
	// Clean up the repository
	if err := sh.Run("git clean %s", gitCleanFlags); err != nil {
		return err
	}

	// Also clean up submodules if we can
	if gitSubmodules {
		if err := sh.Run("git submodule foreach --recursive git clean %s", gitCleanFlags); err != nil {
			return err
		}
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
	output, err := sh.RunAndCapture("git submodule foreach --recursive git remote get-url origin")
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
