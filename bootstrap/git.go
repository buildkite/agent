package bootstrap

import (
	"bufio"
	"bytes"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/shellwords"
)

const (
	gitErrorCheckout = iota
	gitErrorCheckoutReferenceIsNotATree
	gitErrorClone
	gitErrorFetch
	gitErrorClean
	gitErrorCleanSubmodules
)

type gitError struct {
	error
	Type int
}

func gitCheckout(sh *shell.Shell, gitCheckoutFlags, reference string) error {
	individualCheckoutFlags, err := shellwords.Split(gitCheckoutFlags)
	if err != nil {
		return err
	}

	commandArgs := []string{"checkout"}
	commandArgs = append(commandArgs, individualCheckoutFlags...)
	commandArgs = append(commandArgs, reference)

	if err = sh.Run("git", commandArgs...); err != nil {
		if strings.HasPrefix(err.Error(), `fatal: reference is not a tree: `) {
			return &gitError{error: err, Type: gitErrorCheckoutReferenceIsNotATree}
		}
		return &gitError{error: err, Type: gitErrorCheckout}
	}

	return nil
}

func gitClone(sh *shell.Shell, gitCloneFlags, repository, dir string) error {
	individualCloneFlags, err := shellwords.Split(gitCloneFlags)
	if err != nil {
		return err
	}

	commandArgs := []string{"clone"}
	commandArgs = append(commandArgs, individualCloneFlags...)
	commandArgs = append(commandArgs, "--", repository, dir)

	if err = sh.Run("git", commandArgs...); err != nil {
		return &gitError{error: err, Type: gitErrorClone}
	}

	return nil
}

func gitClean(sh *shell.Shell, gitCleanFlags string) error {
	individualCleanFlags, err := shellwords.Split(gitCleanFlags)
	if err != nil {
		return err
	}

	commandArgs := []string{"clean"}
	commandArgs = append(commandArgs, individualCleanFlags...)

	if err = sh.Run("git", commandArgs...); err != nil {
		return &gitError{error: err, Type: gitErrorClean}
	}

	return nil
}

func gitCleanSubmodules(sh *shell.Shell, gitCleanFlags string) error {
	individualCleanFlags, err := shellwords.Split(gitCleanFlags)
	if err != nil {
		return err
	}

	gitCleanCommand := strings.Join(append([]string{"git", "clean"}, individualCleanFlags...), " ")
	commandArgs := append([]string{"submodule", "foreach", "--recursive"}, gitCleanCommand)

	if err = sh.Run("git", commandArgs...); err != nil {
		return &gitError{error: err, Type: gitErrorCleanSubmodules}
	}

	return nil
}

func gitFetch(sh *shell.Shell, gitFetchFlags, repository string, refSpec ...string) error {
	individualFetchFlags, err := shellwords.Split(gitFetchFlags)
	if err != nil {
		return err
	}

	commandArgs := []string{"fetch"}
	commandArgs = append(commandArgs, individualFetchFlags...)
	commandArgs = append(commandArgs, "--") // terminate arg parsing; only repository & refspecs may follow.
	commandArgs = append(commandArgs, repository)

	for _, r := range refSpec {
		individualRefSpecs, err := shellwords.Split(r)
		if err != nil {
			return err
		}
		commandArgs = append(commandArgs, individualRefSpecs...)
	}

	if err = sh.Run("git", commandArgs...); err != nil {
		return &gitError{error: err, Type: gitErrorFetch}
	}

	return nil
}

func gitEnumerateSubmoduleURLs(sh *shell.Shell) ([]string, error) {
	urls := []string{}

	// The output of this command looks like:
	// submodule.bitbucket-git-docker-example.url\ngit@bitbucket.org:lox24/docker-example.git\0
	// submodule.bitbucket-https-docker-example.url\nhttps://lox24@bitbucket.org/lox24/docker-example.git\0
	// submodule.github-git-docker-example.url\ngit@github.com:buildkite/docker-example.git\0
	// submodule.github-https-docker-example.url\nhttps://github.com/buildkite/docker-example.git\0
	output, err := sh.RunAndCapture(
		"git", "config", "--file", ".gitmodules", "--null", "--get-regexp", "submodule\\..+\\.url")
	if err != nil {
		return nil, err
	}

	// splits lines on null-bytes to gracefully handle line endings and repositories with newlines
	lines := strings.Split(strings.TrimRight(output, "\x00"), "\x00")

	// process each line
	for _, line := range lines {
		tokens := strings.SplitN(line, "\n", 2)
		if len(tokens) != 2 {
			return nil, fmt.Errorf("Failed to parse .gitmodules line %q", line)
		}
		urls = append(urls, tokens[1])
	}

	return urls, nil
}

func gitRevParseInWorkingDirectory(sh *shell.Shell, workingDirectory string, extraRevParseArgs ...string) (string, error) {
	gitDirectory := filepath.Join(workingDirectory, ".git")

	revParseArgs := []string{"--git-dir", gitDirectory, "--work-tree", workingDirectory, "rev-parse"}
	revParseArgs = append(revParseArgs, extraRevParseArgs...)

	return sh.RunAndCapture("git", revParseArgs...)
}

var (
	hasSchemePattern  = regexp.MustCompile("^[^:]+://")
	scpLikeURLPattern = regexp.MustCompile("^([^@]+@)?([^:]{2,}):/?(.+)$")
)

// parseGittableURL parses and converts a git repository url into a url.URL
func parseGittableURL(ref string) (*url.URL, error) {
	if !hasSchemePattern.MatchString(ref) {
		if scpLikeURLPattern.MatchString(ref) {
			matched := scpLikeURLPattern.FindStringSubmatch(ref)
			user := matched[1]
			host := matched[2]
			path := matched[3]
			ref = fmt.Sprintf("ssh://%s%s/%s", user, host, path)
		} else {
			normalizedRef := strings.Replace(ref, "\\", "/", -1)
			ref = fmt.Sprintf("file:///%s", strings.TrimPrefix(normalizedRef, "/"))
		}
	}
	return url.Parse(ref)
}

// Clean up the SSH host and remove any key identifiers. See:
// git@github.com-custom-identifier:foo/bar.git
// https://buildkite.com/docs/agent/ssh-keys#creating-multiple-ssh-keys
var gitHostAliasRegexp = regexp.MustCompile(`-[a-z0-9\-]+$`)

func resolveGitHost(sh *shell.Shell, host string) string {
	var hostname string
	var port string

	// ask SSH to print its configuration for this host, honouring .ssh/config
	output, err := sh.RunAndCapture("ssh", "-G", host)

	// if we got no error, let's process the output
	if err == nil {
		// split up the ssh -G output by lines
		scanner := bufio.NewScanner(bytes.NewBufferString(output))

		for scanner.Scan() {
			line := scanner.Text()

			// search the ssh -G output for "hostname" and "port" lines
			tokens := strings.SplitN(line, " ", 2)

			// skip any line which isn't a key-value pair
			if len(tokens) != 2 {
				break
			}

			// grab the values we care about
			if tokens[0] == "hostname" {
				hostname = tokens[1]
			} else if tokens[0] == "port" {
				port = tokens[1]
			}

			// if we have both values, we're done here!
			if hostname != "" && port != "" {
				break
			}
		}
	}

	// if we got out of that with a hostname, things worked
	if hostname != "" {
		// if the port is the default, we can leave it off
		if port == "22" {
			return hostname
		}

		// otherwise, output it in hostname:port form
		return fmt.Sprintf("%s:%s", hostname, port)
	}

	// if we got here, either the `-G` flag was unsupported, or ssh -G
	// didn't return a value for hostname (weird!),
	// so we fall back to the old behaviour of just replacing strings
	return gitHostAliasRegexp.ReplaceAllString(host, "")
}
