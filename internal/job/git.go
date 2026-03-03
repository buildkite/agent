package job

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/roko"
	"github.com/buildkite/shellwords"
)

const (
	gitErrorCheckout = iota
	gitErrorCheckoutReferenceIsNotATree
	gitErrorCheckoutRetryClean
	gitErrorClone
	gitErrorFetch
	// exit code 128: broad error, most likely not recoverable.
	gitErrorFetchRetryClean
	// local repo corruption, unrecoverable.
	gitErrorFetchBadObject
	// can happen when just the short commit hash is given.
	gitErrorFetchBadReference
	gitErrorClean
	gitErrorCleanSubmodules
)

const (
	gitErrStrBadObject             = "fatal: bad object"
	gitErrStrBadReference          = "fatal: couldn't find remote ref"
	gitErrStrBadReferencePreGit221 = "fatal: Couldn't find remote ref"
)

var (
	errNoHostname = errors.New("no hostname found")
	errInvalidRef = errors.New("is not a valid git ref format")
)

type gitError struct {
	error
	Type       int
	WasRetried bool
}

func (e *gitError) Unwrap() error {
	return e.error
}

func gitCheckout(ctx context.Context, sh *shell.Shell, gitCheckoutFlags, reference string) error {
	individualCheckoutFlags, err := shellwords.Split(gitCheckoutFlags)
	if err != nil {
		return err
	}
	if !gitCheckRefFormat(reference) {
		return fmt.Errorf("%q %w", reference, errInvalidRef)
	}

	commandArgs := []string{"checkout"}
	commandArgs = append(commandArgs, individualCheckoutFlags...)
	commandArgs = append(commandArgs, reference)

	const badReference = "fatal: reference is not a tree"
	smelt := map[string]bool{badReference: false}

	if err := sh.Command("git", commandArgs...).Run(ctx, shell.WithStringSearch(smelt)); err != nil {
		if smelt[badReference] {
			return &gitError{error: err, Type: gitErrorCheckoutReferenceIsNotATree}
		}

		// 128 is extremely broad, but it seems permissions errors, network unreachable errors etc,
		// don't result in it
		if exitErr := new(exec.ExitError); errors.As(err, &exitErr) && exitErr.ExitCode() == 128 {
			return &gitError{error: err, Type: gitErrorCheckoutRetryClean}
		}

		return &gitError{error: err, Type: gitErrorCheckout}
	}

	return nil
}

func gitClone(ctx context.Context, sh *shell.Shell, gitCloneFlags, repository, dir string) error {
	individualCloneFlags, err := shellwords.Split(gitCloneFlags)
	if err != nil {
		return err
	}

	commandArgs := []string{"clone"}
	commandArgs = append(commandArgs, individualCloneFlags...)
	commandArgs = append(commandArgs, "--", repository, dir)

	if err := sh.Command("git", commandArgs...).Run(ctx); err != nil {
		return &gitError{error: err, Type: gitErrorClone}
	}

	return nil
}

func gitClean(ctx context.Context, sh *shell.Shell, gitCleanFlags string) error {
	individualCleanFlags, err := shellwords.Split(gitCleanFlags)
	if err != nil {
		return err
	}

	commandArgs := []string{"clean"}
	commandArgs = append(commandArgs, individualCleanFlags...)

	if err := sh.Command("git", commandArgs...).Run(ctx); err != nil {
		return &gitError{error: err, Type: gitErrorClean}
	}

	return nil
}

func gitCleanSubmodules(ctx context.Context, sh *shell.Shell, gitCleanFlags string) error {
	individualCleanFlags, err := shellwords.Split(gitCleanFlags)
	if err != nil {
		return err
	}

	gitCleanCommand := strings.Join(append([]string{"git", "clean"}, individualCleanFlags...), " ")
	commandArgs := append([]string{"submodule", "foreach", "--recursive"}, gitCleanCommand)

	if err := sh.Command("git", commandArgs...).Run(ctx); err != nil {
		return &gitError{error: err, Type: gitErrorCleanSubmodules}
	}

	return nil
}

type gitFetchArgs struct {
	Shell         *shell.Shell // The shell to run the command in
	GitFlags      string       // Global git flags to pass to the command
	GitFetchFlags string       // Flags to pass to the fetch command
	Repository    string       // The remote to fetch from
	Retry         bool         // Whether to retry the fetch on certain errors
	RefSpecs      []string     // Refspecs to fetch
}

func gitFetch(ctx context.Context, args gitFetchArgs) error {
	// Build the command: git [global gitFlags] fetch [fetchFlags] -- [repository] [refspecs...]
	commandArgs := []string{}

	if args.GitFlags != "" {
		parts, err := shellwords.Split(args.GitFlags)
		if err != nil {
			return fmt.Errorf("failed to parse gitFlags: %w", err)
		}
		commandArgs = append(commandArgs, parts...)
	}

	commandArgs = append(commandArgs, "fetch")

	if args.GitFetchFlags != "" {
		parts, err := shellwords.Split(args.GitFetchFlags)
		if err != nil {
			return fmt.Errorf("failed to parse gitFetchFlags: %w", err)
		}
		commandArgs = append(commandArgs, parts...)
	}

	commandArgs = append(commandArgs, "--", args.Repository)

	// Parse and append all refspecs
	for _, r := range args.RefSpecs {
		individualRefSpecs, err := shellwords.Split(r)
		if err != nil {
			return err
		}
		commandArgs = append(commandArgs, individualRefSpecs...)
	}

	smelt := map[string]bool{
		gitErrStrBadObject:             false,
		gitErrStrBadReference:          false,
		gitErrStrBadReferencePreGit221: false,
	}

	// The retry logic is used to handle rare cases where a commit ref is not yet available
	// remotely (e.g. async ref creation), and retrying `git fetch` can resolve it.
	// This is *not* always desirable—some call sites have their own retry mechanisms,
	// so the retry must be opt-in to avoid nested retries and increased test flakiness.
	retrier := roko.NewRetrier(
		roko.WithStrategy(roko.Constant(0)),
		roko.WithMaxAttempts(1),
	)

	if args.Retry {
		retrier = roko.NewRetrier(
			roko.WithStrategy(roko.ExponentialSubsecond(1*time.Second)),
			roko.WithMaxAttempts(10), // 10 attempts will take ~2 minutes 17s
			roko.WithJitter(),
		)
	}

	return retrier.DoWithContext(ctx, func(retrier *roko.Retrier) error {
		if err := args.Shell.Command("git", commandArgs...).Run(ctx, shell.WithStringSearch(smelt)); err != nil {
			// "fatal: [Cc]ouldn't find remote ref" happens when the remote ref does not exist (e.g. a branch that was deleted)
			// Sometimes we want to wait for the remote ref to be created (eg in the case of the PR HEAD ref `refs/pulls/123/head
			// that github creates asynchronously), so this case gets retried -- we don't call r.Break()
			if smelt[gitErrStrBadReference] || smelt[gitErrStrBadReferencePreGit221] {
				args.Shell.Commentf("%s", retrier)
				return &gitError{error: err, Type: gitErrorFetchBadReference, WasRetried: args.Retry}
			}

			// "fatal: bad object" can happen when the local repo in the checkout
			// directory is corrupted, not just the remote or the mirror.
			// When using git mirrors, the existing checkout directory might have a
			// reference to an object that it expects in the mirror, but the mirror
			// no longer contains it (for whatever reason).
			// See the NOTE under --shared at https://git-scm.com/docs/git-clone.
			if smelt[gitErrStrBadObject] {
				retrier.Break()
				return &gitError{error: err, Type: gitErrorFetchBadObject}
			}

			// 128 is extremely broad, but it seems permissions errors, network unreachable errors etc,
			// don't result in it
			if exitErr := new(exec.ExitError); errors.As(err, &exitErr) && exitErr.ExitCode() == 128 {
				retrier.Break()
				return &gitError{error: err, Type: gitErrorFetchRetryClean}
			}

			return &gitError{error: err, Type: gitErrorFetch, WasRetried: args.Retry}
		}
		return nil
	})
}

func gitEnumerateSubmoduleURLs(ctx context.Context, sh *shell.Shell) ([]string, error) {
	urls := []string{}

	// The output of this command looks like:
	// submodule.bitbucket-git-docker-example.url\ngit@bitbucket.org:lox24/docker-example.git\0
	// submodule.bitbucket-https-docker-example.url\nhttps://lox24@bitbucket.org/lox24/docker-example.git\0
	// submodule.github-git-docker-example.url\ngit@github.com:buildkite/docker-example.git\0
	// submodule.github-https-docker-example.url\nhttps://github.com/buildkite/docker-example.git\0
	output, err := sh.Command("git", "config", "--file", ".gitmodules", "--null", "--get-regexp", `submodule\..+\.url`).RunAndCaptureStdout(ctx)
	if err != nil {
		return nil, err
	}

	// splits lines on null-bytes to gracefully handle line endings and repositories with newlines
	lines := strings.SplitSeq(strings.TrimRight(output, "\x00"), "\x00")

	// process each line
	for line := range lines {
		tokens := strings.SplitN(line, "\n", 2)
		if len(tokens) != 2 {
			return nil, fmt.Errorf("failed to parse .gitmodules line %q", line)
		}
		urls = append(urls, tokens[1])
	}

	return urls, nil
}

func gitRevParseInWorkingDirectory(ctx context.Context, sh *shell.Shell, workingDirectory string, extraRevParseArgs ...string) (string, error) {
	gitDirectory := filepath.Join(workingDirectory, ".git")

	revParseArgs := []string{"--git-dir", gitDirectory, "--work-tree", workingDirectory, "rev-parse"}
	revParseArgs = append(revParseArgs, extraRevParseArgs...)

	return sh.Command("git", revParseArgs...).RunAndCaptureStdout(ctx)
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
			normalizedRef := strings.ReplaceAll(ref, "\\", "/")
			ref = fmt.Sprintf("file:///%s", strings.TrimPrefix(normalizedRef, "/"))
		}
	}
	return url.Parse(ref)
}

// Clean up the SSH host and remove any key identifiers. See:
// git@github.com-custom-identifier:foo/bar.git
// https://buildkite.com/docs/agent/ssh-keys#creating-multiple-ssh-keys
var gitHostAliasRegexp = regexp.MustCompile(`-[a-z0-9\-]+$`)

func resolveGitHost(ctx context.Context, sh *shell.Shell, host string) string {
	// ask SSH to print its configuration for this host, honouring .ssh/config
	output, err := sh.Command("ssh", "-G", host).RunAndCaptureStdout(ctx)
	if err != nil {
		// fall back to the old behaviour of just replacing strings
		return gitHostAliasRegexp.ReplaceAllString(host, "")
	}

	// if we got no error, let's process the output
	h, err := hostFromSSHG(output)
	if err != nil {
		// so we fall back to the old behaviour of just replacing strings
		return gitHostAliasRegexp.ReplaceAllString(host, "")
	}
	return h
}

func hostFromSSHG(sshconf string) (string, error) {
	var hostname, port string

	// split up the ssh -G output by lines
	scanner := bufio.NewScanner(strings.NewReader(sshconf))
	for scanner.Scan() {
		line := scanner.Text()

		// search the ssh -G output for "hostname" and "port" lines
		tokens := strings.SplitN(line, " ", 2)

		// skip any line which isn't a key-value pair
		if len(tokens) != 2 {
			continue
		}

		// grab the values we care about
		switch tokens[0] {
		case "hostname":
			hostname = tokens[1]
		case "port":
			port = tokens[1]
		}

		// if we have both values, we're done here!
		if hostname != "" && port != "" {
			break
		}
	}

	if hostname == "" {
		// if we got here, either the `-G` flag was unsupported, or ssh -G
		// didn't return a value for hostname (weird!)
		return "", errNoHostname
	}

	// if we got out of that with a hostname, things worked
	// if the port is the default, we can leave it off
	if port == "" || port == "22" {
		return hostname, nil
	}

	// otherwise, output it in hostname:port form
	return net.JoinHostPort(hostname, port), nil
}

// gitCheckRefFormatDenyRegexp is a pattern used by gitCheckRefFormat().
// Numbered rules are from git 2.28.0's `git help check-ref-format`.
// Not covered by this implementation:
//  1. They can include slash / for hierarchical (directory) grouping, but
//     no slash-separated component can begin with a dot .  or end with the
//     sequence .lock
//  2. They must contain at least one /. This enforces the presence of a
//     category like heads/, tags/, and so on, but the actual names are not
//     restricted. If the --allow-onelevel option is used, this rule is waived
//  5. They cannot have question-mark ?, asterisk *, or open bracket [
//     anywhere. See the --refspec-pattern option below for an exception to
//     this rule
//  6. They cannot begin or end with a slash / or contain multiple
//     consecutive slashes (see the --normalize option below for an exception
//     to this rule)
//  8. They cannot contain a sequence @{.
var gitCheckRefFormatDenyRegexp = regexp.MustCompile(strings.Join([]string{
	`\.\.`,        //  3. cannot have two consecutive dots .. anywhere
	`[[:cntrl:]]`, //  4. cannot have ASCII control characters (In other words, bytes whose values are lower than \040, or \177 DEL) ...
	`[ ~^:]`,      //  4. cannot have ... space, tilde ~, caret ^, or colon : anywhere
	`\.$`,         //  7. cannot end with a dot .
	`^@$`,         //  9. cannot be the single character @.
	`\\`,          // 10. cannot contain a \
	`^-`,          // bonus: disallow refs that would be interpreted as command options/flags
}, "|"))

// gitCheckRefFormat is a simplified version of `git check-ref-format`.
// It mostly assumes --allow-onelevel. In other words, no need for refs/heads/… prefix.
// It is more permissive than the canonical implementation.
// https://git-scm.com/docs/git-check-ref-format
func gitCheckRefFormat(ref string) bool {
	return !gitCheckRefFormatDenyRegexp.MatchString(ref)
}
