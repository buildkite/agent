package job

import (
	"context"
	"encoding/base64"
	"errors"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/logger"
)

var (
	acceleratorRepositoryPath = regexp.MustCompile(`^(/[a-zA-Z0-9._~-]+)+\.git$`)
	acceleratorHostLabel      = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
)

// ValidateGitAcceleratorURL requires an HTTPS authority without a repo path.
func ValidateGitAcceleratorURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" || u.ForceQuery || u.RawQuery != "" || strings.Contains(raw, "#") {
		return errors.New("--git-accelerator-url must be an HTTPS origin with an optional port")
	}
	if port := u.Port(); port != "" || strings.HasSuffix(u.Host, ":") {
		if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
			return errors.New("--git-accelerator-url has an invalid port")
		}
	}
	return nil
}

// ValidateGitAcceleratorHosts accepts only bare DNS hostnames, never URL patterns.
func ValidateGitAcceleratorHosts(hosts []string) error {
	for _, host := range hosts {
		if !validAcceleratorOriginHost(host) {
			return errors.New("--git-accelerator-hosts requires exact DNS hostnames")
		}
	}
	return nil
}

func validAcceleratorOriginHost(host string) bool {
	if _, err := netip.ParseAddr(host); err == nil || len(host) > 253 {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if !acceleratorHostLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func (e *Executor) acceleratorRepositoryURL() string {
	if ValidateGitAcceleratorURL(e.GitAcceleratorURL) != nil {
		return ""
	}
	u, err := url.Parse(e.Repository)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.Host != u.Hostname() || u.User != nil ||
		u.RawPath != "" || u.ForceQuery || u.RawQuery != "" || strings.Contains(e.Repository, "#") ||
		strings.Contains(e.Repository, "%") || !acceleratorRepositoryPath.MatchString(u.Path) {
		return ""
	}
	if !validAcceleratorOriginHost(u.Host) {
		return ""
	}
	if len(e.GitAcceleratorHosts) > 0 && !slices.ContainsFunc(e.GitAcceleratorHosts, func(host string) bool {
		return strings.EqualFold(host, u.Host)
	}) {
		return ""
	}
	for part := range strings.SplitSeq(strings.TrimSuffix(u.Path, ".git")[1:], "/") {
		if part == "" || part == "." || part == ".." {
			return ""
		}
	}
	return strings.TrimSuffix(e.GitAcceleratorURL, "/") + "/" + strings.ToLower(u.Host) + u.Path
}

// acceleratorGit prepares a one-command transport environment. In particular,
// job-supplied trace, proxy, helper and TLS overrides do not apply to it.
func (e *Executor) acceleratorGit(ctx context.Context, attempt *remoteMirrorAttempt, gitDir string, flags []string) (*shell.Shell, []string, error) {
	environ := env.FromMap(map[string]string{
		"GIT_CEILING_DIRECTORIES": filepath.Dir(e.shell.Getwd()),
		"GIT_CONFIG_NOSYSTEM":     "1",
		"GIT_CONFIG_GLOBAL":       os.DevNull,
		"GIT_TERMINAL_PROMPT":     "0",
		"GIT_ALLOW_PROTOCOL":      "https",
	})
	// Use the operator's executable search path rather than the job's PATH.
	for _, key := range []string{"PATHEXT", "SystemRoot", "WINDIR", "TEMP", "TMP", "TMPDIR"} {
		if value, ok := os.LookupEnv(key); ok {
			environ.Set(key, value)
		}
	}
	path := e.GitAcceleratorPath
	if path == "" { // Standalone bootstrap, without an agent job runner.
		path = os.Getenv("PATH")
	}
	// Don't look for git in the checkout via empty or relative path entries.
	var absolutePaths []string
	for _, dir := range filepath.SplitList(path) {
		if filepath.IsAbs(dir) {
			absolutePaths = append(absolutePaths, dir)
		}
	}
	if len(absolutePaths) == 0 {
		return nil, nil, errors.New("no absolute accelerator Git search path")
	}
	environ.Set("PATH", strings.Join(absolutePaths, string(os.PathListSeparator)))
	sh := e.shell.CloneWithEnv(environ)
	if gitDir != "" {
		// System and global config are disabled, but fetch still reads the
		// destination repo's config. Only the ordinary clone config is safe here.
		keys, err := sh.Command("git", "--git-dir", gitDir, "config", "--local", "--no-includes", "--name-only", "--list").RunAndCaptureStdout(ctx, shell.AlwaysHidePrompt(), shell.ShowStderr(false))
		if err != nil {
			return nil, nil, errors.New("could not inspect local Git config")
		}
		for key := range strings.SplitSeq(keys, "\n") {
			switch key {
			case "core.repositoryformatversion", "core.filemode", "core.bare", "core.logallrefupdates", "core.symlinks", "core.ignorecase", "core.precomposeunicode",
				"remote.origin.url", "remote.origin.fetch", "remote.origin.mirror", "remote.origin.tagopt", remoteMirrorPromisorMarkerKey:
				continue
			}
			if strings.HasPrefix(key, "branch.") && (strings.HasSuffix(key, ".remote") || strings.HasSuffix(key, ".merge")) {
				continue
			}
			return nil, nil, errors.New("local Git config is not supported by accelerator")
		}
	}
	flags = append(flags, "-c", "protocol.version=2", "-c", "http.followRedirects=false", "-c", "core.hooksPath="+os.DevNull)
	if !e.useRepositoryProviderGitCredentials() {
		return sh, flags, nil
	}
	client := api.NewClient(logger.Discard, api.Config{
		Endpoint:     e.shell.Env.GetString("BUILDKITE_AGENT_ENDPOINT", ""),
		Token:        e.shell.Env.GetString("BUILDKITE_AGENT_ACCESS_TOKEN", ""),
		DisableHTTP2: e.shell.Env.GetBool("BUILDKITE_NO_HTTP2", false),
	})
	credentialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	creds, _, err := client.GenerateRepositoryCredentials(credentialCtx, e.canonicalRepository, e.JobID)
	if err != nil || creds.Token == "" {
		return nil, nil, errors.New("managed repository credentials unavailable for accelerator")
	}
	username := creds.Username
	if username == "" {
		username = "token"
	}
	if strings.Contains(username, ":") || strings.ContainsAny(username+creds.Token, "\r\n\x00") {
		return nil, nil, errors.New("invalid managed repository credentials for accelerator")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + creds.Token))
	e.redactors.Add(creds.Token, encoded)
	environ.Set("BUILDKITE_ACCELERATOR_AUTHORIZATION", "Authorization: Basic "+encoded)
	return sh, append(flags, "--config-env=http."+attempt.url+".extraHeader=BUILDKITE_ACCELERATOR_AUTHORIZATION"), nil
}

func (e *Executor) cloneFromAccelerator(ctx context.Context, attempt *remoteMirrorAttempt, flags []string, destination string, bare bool) error {
	sh, gitFlags, err := e.acceleratorGit(ctx, attempt, "", []string{
		"-c", "http.lowSpeedLimit=1000", "-c", "http.lowSpeedTime=" + strconv.Itoa(remoteMirrorLowSpeedTime),
	})
	if err != nil {
		return err
	}
	flags = append(append([]string{}, flags...), "--template=")
	if !bare {
		flags = append(flags, "--no-checkout")
	}
	return gitClone(withoutGitErrorCapture(ctx), sh, gitFlags, flags, attempt.url, destination, shell.AlwaysHidePrompt())
}
