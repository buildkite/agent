package job

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
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
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/agent/v4/tracetools"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const acceleratorAuthorizationEnv = "BUILDKITE_ACCELERATOR_AUTHORIZATION"

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
func (e *Executor) acceleratorGit(ctx context.Context, attempt *remoteMirrorAttempt, gitDir string, flags []string, anonymous bool) (*shell.Shell, []string, error) {
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
	if anonymous || !e.useRepositoryProviderGitCredentials() {
		return sh, flags, nil
	}
	client := api.NewClient(logger.Discard, api.Config{
		Endpoint:     e.shell.Env.GetString("BUILDKITE_AGENT_ENDPOINT", ""),
		Token:        e.shell.Env.GetString("BUILDKITE_AGENT_ACCESS_TOKEN", ""),
		DisableHTTP2: e.shell.Env.GetBool("BUILDKITE_NO_HTTP2", false),
	})
	credentialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	credentialSpan, credentialCtx := e.traceOpSpan(credentialCtx, "git.accelerator.credentials")
	creds, _, err := client.GenerateRepositoryCredentials(credentialCtx, e.canonicalRepository, e.JobID)
	if err != nil || creds.Token == "" {
		if err := ctx.Err(); err != nil {
			credentialSpan.SetStatus(codes.Error, "credential request interrupted")
			credentialSpan.End()
			return nil, nil, err
		}
		credentialSpan.SetAttributes(attribute.String("outcome", "anonymous-fallback"))
		credentialSpan.End()
		e.shell.Commentf("Managed repository credentials unavailable; trying Git Accelerator anonymously")
		return sh, flags, nil
	}
	credentialSpan.End()
	username := creds.Username
	if username == "" {
		username = "token"
	}
	if strings.Contains(username, ":") || strings.ContainsAny(username+creds.Token, "\r\n\x00") {
		return nil, nil, errors.New("invalid managed repository credentials for accelerator")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + creds.Token))
	e.redactors.Add(creds.Token, encoded)
	environ.Set(acceleratorAuthorizationEnv, "Authorization: Basic "+encoded)
	return sh, append(flags, "--config-env=http."+attempt.url+".extraHeader="+acceleratorAuthorizationEnv), nil
}

func (e *Executor) cloneFromAccelerator(ctx context.Context, attempt *remoteMirrorAttempt, flags []string, destination string, bare bool) error {
	transportFlags := []string{
		"-c", "http.lowSpeedLimit=1000", "-c", "http.lowSpeedTime=" + strconv.Itoa(remoteMirrorLowSpeedTime),
	}
	sh, gitFlags, err := e.acceleratorGit(ctx, attempt, "", transportFlags, false)
	if err != nil {
		return err
	}
	err = e.runAcceleratorClone(ctx, attempt, sh, gitFlags, flags, destination, bare, false)
	if err == nil || ctx.Err() != nil {
		return err
	}
	var cloneErr *gitError
	if errors.As(err, &cloneErr) && cloneErr.Type == gitErrorCloneTimeout {
		return err
	}
	// Git does not expose the HTTP status directly. Confirm the authenticated
	// discovery request is challenged before retrying without credentials.
	auth := sh.Env.GetString(acceleratorAuthorizationEnv, "")
	if auth == "" || !acceleratorChallengesToken(ctx, attempt.url, auth) {
		return err
	}
	e.shell.Commentf("Git Accelerator rejected managed repository credentials; retrying anonymously")
	// Git cleans up the failed clone before returning. Prepare a new isolated
	// environment so neither the retry's Git nor CloneKit receives the token.
	sh, gitFlags, err = e.acceleratorGit(ctx, attempt, "", transportFlags, true)
	if err != nil {
		return err
	}
	return e.runAcceleratorClone(ctx, attempt, sh, gitFlags, flags, destination, bare, true)
}

func acceleratorChallengesToken(ctx context.Context, source, auth string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source+"/info/refs?service=git-upload-pack", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Git-Protocol", "version=2")
	req.Header.Set("Authorization", strings.TrimPrefix(auth, "Authorization: "))
	client := acceleratorHTTPClient()
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusUnauthorized && strings.EqualFold(resp.Header.Get("WWW-Authenticate"), `Basic realm="git-accelerator"`)
}

func (e *Executor) runAcceleratorClone(ctx context.Context, attempt *remoteMirrorAttempt, sh *shell.Shell, gitFlags, flags []string, destination string, bare, retry bool) error {
	flags = append(append([]string{}, flags...), "--template=")
	if !bare {
		flags = append(flags, "--no-checkout")
	}
	cloneDir := destination
	if !filepath.IsAbs(cloneDir) {
		cloneDir = filepath.Join(sh.Getwd(), cloneDir)
	}
	var seed string
	if experiments.IsEnabled(ctx, experiments.GitAcceleratorCloneKit) && !e.checkoutAlreadyExists() &&
		len(e.GitSparseCheckoutPaths) == 0 && (e.GitCloneFlags == "" || e.GitCloneFlags == "-v") {
		seedOnly := !bare && e.PullRequest == "false" && experiments.IsEnabled(ctx, experiments.GitAcceleratorCloneKitSeedOnly)
		if seedOnly {
			// Like git clone, only initialize an empty destination.
			entries, err := os.ReadDir(cloneDir)
			seedOnly = err == nil && len(entries) == 0
		}
		if seedOnly && e.Commit == "HEAD" && e.resolvedCheckoutHead == "" {
			switch e.GitFetchFlags {
			case "", "-v --prune", "--no-tags":
				e.resolvedCheckoutHead = e.resolveAcceleratorHead(ctx, sh, gitFlags, attempt.url)
			}
		}
		// Stage beside the clone so its destination stays empty and the pack
		// can normally be installed by rename on the same filesystem.
		if temp, err := os.MkdirTemp(filepath.Dir(cloneDir), "buildkite-clonekit-"); err == nil {
			cleanup := func() {
				if err := os.RemoveAll(temp); err != nil {
					e.shell.Warningf("Could not remove temporary CloneKit seed")
				}
			}
			if staging := filepath.Join(temp, "seed.git"); e.tryAcceleratorCloneKit(ctx, sh, attempt.url, staging) {
				seed = staging
				defer cleanup()
				if seedOnly {
					commit := e.Commit
					if e.resolvedCheckoutHead != "" {
						commit = e.resolvedCheckoutHead
						if !hasGitCommit(ctx, sh, seed, commit) {
							if err := e.fetchAcceleratorHeadIntoSeed(ctx, sh, gitFlags, attempt.url, seed, commit); err != nil {
								return err
							}
						}
					} else if !fullSHA1ObjectID.MatchString(commit) || !hasGitCommit(ctx, sh, seed, commit) {
						commit = ""
					}
					if commit != "" {
						return e.traceOp(ctx, "git.accelerator.clonekit.seed_only", func(ctx context.Context) error {
							if err := e.initializeCloneKitCheckout(ctx, sh, seed, cloneDir, commit); err != nil {
								return err
							}
							e.shell.Commentf("Git accelerator CloneKit: initialized checkout from seed without cloning")
							return nil
						})
					}
				}
				flags = append(flags, "--reference", seed)
			} else {
				cleanup()
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if err := e.traceCloneCommand(ctx, "git-accelerator", seed != "", retry, false, func(ctx context.Context) error {
		return gitClone(withoutGitErrorCapture(ctx), sh, gitFlags, flags, attempt.url, destination, shell.AlwaysHidePrompt())
	}); err != nil {
		return err
	}
	if seed != "" {
		gitDir := cloneDir
		if !bare {
			gitDir = filepath.Join(gitDir, ".git")
		}
		span, _ := e.traceOpSpan(ctx, "git.accelerator.clonekit.install")
		err := installCloneKitPack(seed, gitDir)
		if err == nil {
			err = os.Remove(filepath.Join(gitDir, "objects", "info", "alternates"))
		}
		if err != nil {
			span.SetStatus(codes.Error, "pack installation failed")
		}
		span.End()
		if err != nil {
			return err
		}
	}
	if !bare && e.Commit == "HEAD" && e.resolvedCheckoutHead == "" {
		// Keep origin's tracking rules, but rewrite its URL for this command only.
		attempt.acceleratorHeadFetch = &gitFetchArgs{
			Shell:      sh,
			GitFlags:   append(gitFlags, "-c", "url."+attempt.url+".insteadOf="+e.Repository),
			Repository: "origin",
			HidePrompt: true,
		}
	}
	return nil
}

func (e *Executor) resolveAcceleratorHead(ctx context.Context, sh *shell.Shell, gitFlags []string, source string) string {
	span, ctx := e.traceOpSpan(ctx, "git.accelerator.resolve_head")
	defer span.End()
	ctx, cancel := context.WithTimeout(ctx, remoteMirrorProbeTimeout)
	defer cancel()
	ref := "refs/heads/" + e.Branch
	args := append(slices.Clone(gitFlags), "ls-remote", "--exit-code", "--refs", "--", source, ref)
	out, err := sh.Command("git", args...).RunAndCaptureStdout(ctx, shell.AlwaysHidePrompt(), shell.ShowStderr(false))
	sha, remoteRef, _ := strings.Cut(strings.TrimSpace(out), "\t")
	if err != nil || remoteRef != ref || !fullSHA1ObjectID.MatchString(sha) {
		span.SetAttributes(attribute.String("outcome", "fallback"))
		e.shell.Commentf("Git Accelerator could not resolve branch HEAD early; using normal HEAD checkout")
		return ""
	}
	span.SetAttributes(attribute.String("outcome", "resolved"))
	e.shell.Commentf("Resolved branch HEAD through Git Accelerator to %s", sha)
	return sha
}

func (e *Executor) fetchAcceleratorHeadIntoSeed(ctx context.Context, sh *shell.Shell, gitFlags []string, source, seed, commit string) error {
	return e.traceOp(ctx, "git.accelerator.clonekit.catch_up", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, remoteMirrorProbeTimeout)
		defer cancel()
		// Keep even a tiny fetch as a pack: seed installation moves only pack files.
		args := append(slices.Clone(gitFlags), "--git-dir", seed, "-c", "fetch.unpackLimit=1", "fetch",
			"--no-tags", "--no-recurse-submodules", "--no-auto-maintenance", "--", source, commit)
		return cloneKitGit(ctx, sh, args...)
	})
}

func (e *Executor) initializeCloneKitCheckout(ctx context.Context, sh *shell.Shell, seed, cloneDir, commit string) error {
	if err := cloneKitGit(ctx, sh, "init", "--template=", "--object-format=sha1", "--", cloneDir); err != nil {
		return fmt.Errorf("initializing CloneKit checkout: %w", err)
	}
	gitDir := filepath.Join(cloneDir, ".git")
	if err := installCloneKitPack(seed, gitDir); err != nil {
		return err
	}
	// Keep canonical origin for normal branch verification, but don't auto-follow
	// tags. A detached HEAD lets Git negotiate from the seed if verification
	// needs to fetch the branch tip; the normal checkout populates the worktree.
	if err := cloneKitGit(ctx, sh, "--git-dir", gitDir, "remote", "add", "--no-tags", "origin", e.Repository); err != nil {
		return fmt.Errorf("setting CloneKit checkout origin: %w", err)
	}
	return cloneKitGit(ctx, sh, "--git-dir", gitDir, "update-ref", "--no-deref", "HEAD", commit)
}

// installCloneKitPack takes ownership of the verified pack without repacking.
func installCloneKitPack(seed, gitDir string) error {
	seedPackDir := filepath.Join(seed, "objects", "pack")
	objects := filepath.Join(gitDir, "objects")
	files, err := os.ReadDir(seedPackDir)
	if err != nil {
		return fmt.Errorf("reading CloneKit pack files: %w", err)
	}
	for _, file := range files {
		if err := os.Rename(filepath.Join(seedPackDir, file.Name()), filepath.Join(objects, "pack", file.Name())); err != nil {
			return fmt.Errorf("installing CloneKit pack file: %w", err)
		}
	}
	return nil
}

func acceleratorHTTPClient() *http.Client {
	// Like accelerator Git, do not use an ambient proxy or follow redirects.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (e *Executor) tryAcceleratorCloneKit(parent context.Context, sh *shell.Shell, source, staging string) bool {
	span, ctx := e.traceOpSpan(parent, "git.accelerator.clonekit")
	ctx, cancel := context.WithTimeout(ctx, cloneKitSeedTimeout)
	defer cancel()
	started := time.Now()
	stage, outcome := "manifest", "error"
	defer func() {
		span.SetAttributes(attribute.String("stage", stage), attribute.String("outcome", outcome), attribute.Float64("duration_seconds", time.Since(started).Seconds()))
		tracetools.FinishWithError(span, nil)
		e.shell.Commentf("Git accelerator CloneKit: %s (%s)", outcome, stage)
	}()
	client := acceleratorHTTPClient()
	defer client.CloseIdleConnections()
	seedEnv := sh.Env.Copy()
	auth := strings.TrimPrefix(seedEnv.Remove(acceleratorAuthorizationEnv), "Authorization: ")
	manifestSpan, manifestCtx := e.traceOpSpan(ctx, "git.accelerator.clonekit.manifest")
	m, err := fetchCloneKitManifest(manifestCtx, client, source+"/clone-kit", func(req *http.Request) {
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
	})
	if errors.Is(err, errCloneKitMiss) {
		manifestSpan.SetAttributes(attribute.String("outcome", "miss"))
	} else if err != nil {
		manifestSpan.SetStatus(codes.Error, "manifest request failed")
	}
	manifestSpan.End()
	if err == nil {
		authority, _ := url.Parse(source) // validated when the accelerator was selected
		for _, file := range m.Files {
			u, _ := url.Parse(file.URL) // the manifest parser already validated its HTTPS URL
			if !strings.EqualFold(u.Host, authority.Host) {
				return false
			}
		}
		stage, err = e.prepareCloneKitSeed(ctx, sh.CloneWithEnv(seedEnv), client, m, staging, "git.accelerator.clonekit")
	}
	switch {
	case parent.Err() != nil:
		outcome = "cancelled"
	case ctx.Err() != nil:
		outcome = "timeout"
	case err == nil:
		outcome = "hit"
	case errors.Is(err, errCloneKitMiss):
		outcome = "miss"
	}
	return outcome == "hit"
}
