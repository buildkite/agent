package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/agent/plugin"
	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/internal/job/hook"
	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/roko"
	"github.com/buildkite/shellwords"
)

type pluginCheckout struct {
	*plugin.Plugin
	*plugin.Definition

	// Root is the *os.Root for the checkout directory containing
	// the plugin. This is the plugin repo itself for normal plugins, and
	// the build checkout (Executor.checkoutRoot) for vendored plugins.
	Root *os.Root

	// PluginDir is the path within Root that contains the plugin.
	// This is usually "." for normal plugins, and usually some subpath for
	// vendored plugins.
	PluginDir string

	// HooksDir is the path within Root that contains the plugins hooks.
	// This should be equivalent to filepath.Join(PluginDir, "hooks").
	HooksDir string
}

func (e *Executor) hasPlugins() bool {
	return e.Plugins != ""
}

func (e *Executor) preparePlugins() error {
	if !e.hasPlugins() {
		return nil
	}

	e.shell.Headerf("Preparing plugins")

	if e.Debug {
		e.shell.Commentf("Plugin JSON is %s", e.Plugins)
	}

	// Check if we can run plugins (disabled via --no-plugins)
	if !e.PluginsEnabled {
		if !e.LocalHooksEnabled {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-local-hooks`")
		} else if !e.CommandEval {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-command-eval`")
		} else {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-plugins`")
		}
	}

	var err error
	e.plugins, err = plugin.CreateFromJSON(e.Plugins)
	if err != nil {
		return fmt.Errorf("Failed to parse a plugin definition: %w", err)
	}

	if e.Debug {
		e.shell.Commentf("Parsed %d plugins", len(e.plugins))
	}

	return nil
}

func (e *Executor) validatePluginCheckout(ctx context.Context, checkout *pluginCheckout) error {
	if !e.PluginValidation {
		return nil
	}

	if checkout.Definition == nil {
		if e.Debug {
			e.shell.Commentf("Parsing plugin definition for %s from %s", checkout.Plugin.Name(), checkout.PluginDir)
		}

		// parse the plugin definition from the plugin checkout dir
		def, err := plugin.LoadDefinitionFromDir(checkout.Root, checkout.PluginDir)
		if errors.Is(err, plugin.ErrDefinitionNotFound) {
			e.shell.Warningf("Failed to find plugin definition for plugin %s", checkout.Plugin.Name())
			return nil
		}
		if err != nil {
			return err
		}
		checkout.Definition = def
	}

	val := &plugin.Validator{}
	result := val.Validate(ctx, checkout.Definition, checkout.Plugin.Configuration)

	if !result.Valid() {
		e.shell.Headerf("Plugin validation failed for %q", checkout.Plugin.Name())
		json, _ := json.Marshal(checkout.Plugin.Configuration)
		e.shell.Commentf("Plugin configuration JSON is %s", json)
		return result
	}

	e.shell.Commentf("Valid plugin configuration for %q", checkout.Plugin.Name())
	return nil
}

// PluginPhase is where plugins that weren't filtered in the Environment phase are
// checked out and made available to later phases
func (e *Executor) PluginPhase(ctx context.Context) error {
	if len(e.plugins) == 0 {
		if e.Debug {
			e.shell.Commentf("Skipping plugin phase")
		}
		return nil
	}

	checkouts := []*pluginCheckout{}

	// Checkout and validate plugins that aren't vendored
	for _, p := range e.plugins {
		if p.Vendored {
			if e.Debug {
				e.shell.Commentf("Skipping vendored plugin %s", p.Name())
			}
			continue
		}

		checkout, err := e.checkoutPlugin(ctx, p)
		if err != nil {
			return fmt.Errorf("Failed to checkout plugin %s: %w", p.Name(), err)
		}

		err = e.validatePluginCheckout(ctx, checkout)
		if err != nil {
			return err
		}

		checkouts = append(checkouts, checkout)
	}

	// Store the checkouts for future use
	e.pluginCheckouts = checkouts

	// Now we can run plugin environment hooks too
	return e.executePluginHook(ctx, "environment", checkouts)
}

// VendoredPluginPhase is where plugins that are included in the
// checked out code are added
func (e *Executor) VendoredPluginPhase(ctx context.Context) error {
	if !e.hasPlugins() {
		return nil
	}

	vendoredCheckouts := []*pluginCheckout{}

	// Validate vendored plugins
	for _, p := range e.plugins {
		if !p.Vendored {
			continue
		}

		// Check that the plugin exists in the checkout.
		if fi, err := e.checkoutRoot.Stat(p.Location); err != nil || !fi.IsDir() {
			return fmt.Errorf("Vendored plugin path %q must be a directory within the checked-out repository: %w", p.Location, err)
		}

		// Similarly, check that the plugin's hooks exists in the checkout.
		hooksPath := filepath.Join(p.Location, "hooks")
		if fi, err := e.checkoutRoot.Stat(hooksPath); err != nil || !fi.IsDir() {
			return fmt.Errorf("Vendored plugin hooks path %q must be a directory within the checked-out repository: %w", hooksPath, err)
		}

		checkout := &pluginCheckout{
			Plugin:    p,
			Root:      e.checkoutRoot,
			PluginDir: p.Location,
			HooksDir:  hooksPath,
		}

		if err := e.validatePluginCheckout(ctx, checkout); err != nil {
			return err
		}

		vendoredCheckouts = append(vendoredCheckouts, checkout)
	}

	// Finally append our vendored checkouts to the rest for subsequent hooks
	e.pluginCheckouts = append(e.pluginCheckouts, vendoredCheckouts...)

	// Now we can run plugin environment hooks too
	return e.executePluginHook(ctx, "environment", vendoredCheckouts)
}

// Hook types that we should only run one of, but a long-standing bug means that
// we allowed more than one to run (for plugins).
var strictSingleHookTypes = map[string]bool{
	"command":  true,
	"checkout": true,
}

// Executes a named hook on plugins that have it
func (e *Executor) executePluginHook(ctx context.Context, name string, checkouts []*pluginCheckout) error {
	// Command and checkout hooks are a little different, in that we only execute
	// the first one we see. We run the first one, and output a warning for all
	// the subsequent ones.
	hookTypeSeen := make(map[string]bool)

	for i, p := range checkouts {
		// The plugin's hooks must exist within the plugin checkout root.
		hookPath, err := hook.Find(p.Root, p.HooksDir, name)
		if errors.Is(err, os.ErrNotExist) {
			continue // this plugin does not implement this hook
		}
		if err != nil {
			return err
		}

		if strictSingleHookTypes[name] && hookTypeSeen[name] {
			if e.StrictSingleHooks {
				e.shell.Warningf("Ignoring additional %s hook (%s plugin, position %d)",
					name, p.Plugin.Name(), i+1)
				continue
			} else {
				e.shell.Warningf("The additional %s hook (%s plugin, position %d) "+
					"will be ignored in a future version of the agent. To enforce "+
					"single %s hooks now, pass the --strict-single-hooks flag, set "+
					"the environment variable BUILDKITE_STRICT_SINGLE_HOOKS=true, "+
					"or set strict-single-hooks=true in your agent configuration",
					name, p.Plugin.Name(), i+1, name)
			}
		}
		hookTypeSeen[name] = true

		envMap, err := p.ConfigurationToEnvironment()
		if dnerr := (&plugin.DeprecatedNameErrors{}); errors.As(err, &dnerr) {
			e.shell.Headerf("Deprecated environment variables for plugin %s", p.Plugin.Name())
			e.shell.Printf("%s", strings.Join([]string{
				"The way that environment variables are derived from the plugin configuration is changing.",
				"We'll export both the deprecated and the replacement names for now,",
				"You may be able to avoid this by removing consecutive underscore, hyphen, or whitespace",
				"characters in your plugin configuration.",
			}, " "))
			for _, err := range dnerr.Unwrap() {
				e.shell.Printf("%s", err.Error())
			}
		} else if err != nil {
			e.shell.Warningf("Error configuring plugin environment: %s", err)
		}

		if err := e.executeHook(ctx, HookConfig{
			Scope:      HookScopePlugin,
			Name:       name,
			Path:       hookPath,
			Env:        envMap,
			PluginName: p.Plugin.DisplayName(),
			SpanAttributes: map[string]string{
				"plugin.name":        p.Plugin.Name(),
				"plugin.version":     p.Version,
				"plugin.location":    p.Location,
				"plugin.is_vendored": strconv.FormatBool(p.Vendored),
			},
		}); err != nil {
			return err
		}
	}

	return nil
}

// hasPluginHook reports if any plugin has a hook by this name.
func (e *Executor) hasPluginHook(name string) bool {
	return slices.ContainsFunc(e.pluginCheckouts, func(p *pluginCheckout) bool {
		_, err := hook.Find(p.Root, p.HooksDir, name)
		return err == nil
	})
}

// Checkout a given plugin to the plugins directory and return that directory. Each agent worker
// will checkout the plugin to a different directory, so that they don't conflict with each other.
// Because the plugin directory is unique to the agent worker, we don't lock it. However, if
// multiple agent workers have access to the plugin directory, they need to have different names.
func (e *Executor) checkoutPlugin(ctx context.Context, p *plugin.Plugin) (*pluginCheckout, error) {
	// Make sure we have a plugin path before trying to do anything
	if e.PluginsPath == "" {
		return nil, fmt.Errorf("Can't checkout plugin without a `plugins-path`")
	}

	id, err := p.Identifier()
	if err != nil {
		return nil, err
	}

	pluginParentDir := filepath.Join(e.PluginsPath, e.AgentName)

	// Ensure the parent of the plugin directory exists, otherwise we can't move the temp git repo dir
	// into it. The actual file permissions will be reduced by umask, and won't be 0o777 unless the
	// user has manually changed the umask to 0o000
	if err := os.MkdirAll(pluginParentDir, 0o777); err != nil {
		return nil, err
	}

	// Get the subdirectory path if specified in the plugin location
	subdir, err := p.RepositorySubdirectory()
	if err != nil {
		return nil, fmt.Errorf("failed to get repository subdirectory: %w", err)
	}

	// Create a path to the plugin
	pluginDirectory := filepath.Join(pluginParentDir, id)
	pluginGitDirectory := filepath.Join(pluginDirectory, ".git")
	checkout := &pluginCheckout{
		Plugin:    p,
		PluginDir: ".",
		HooksDir:  "hooks",
	}

	// If there's a subdirectory, we'll adjust the paths accordingly
	if subdir != "" {
		checkout.PluginDir = subdir
		checkout.HooksDir = filepath.Join(subdir, "hooks")
	}
	if e.Debug {
		e.shell.Commentf("Plugin checkout paths - PluginDir: %q, HooksDir: %q", checkout.PluginDir, checkout.HooksDir)
	}

	// Route zip plugins to zip handler
	if p.IsZipPlugin() {
		if !experiments.IsEnabled(ctx, experiments.ZipPlugins) {
			return nil, fmt.Errorf("zip plugins require the %q experiment to be enabled", experiments.ZipPlugins)
		}
		e.shell.Commentf("Plugin %q will be downloaded as zip archive", p.DisplayName())
		return checkout, e.checkoutZipPlugin(ctx, p, checkout, pluginDirectory)
	}

	// If there is already a clone, the user may want to ensure it's fresh (e.g., by setting
	// BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH=true).
	//
	// Neither of the obvious options here is very nice.  Either we git-fetch and git-checkout on
	// existing repos, which is probably fast, but it's _surprisingly hard_ to write a really robust
	// chain of Git commands that'll definitely get you a clean version of a given upstream branch
	// ref (the branch might have been force-pushed, the checkout might have become dirty and
	// unmergeable, etc.).  Plus, then we're duplicating a bunch of fetch/checkout machinery and
	// perhaps missing things (like `addRepositoryHostToSSHKnownHosts` which is called down below).
	// Alternatively, we can DRY it up and simply `rm -rf` the plugin directory if it exists, but
	// that means a potentially slow and unnecessary clone on every build step.  Sigh.  I think the
	// tradeoff is favourable for just blowing away an existing clone if we want least-hassle
	// guarantee that the user will get the latest version of their plugin branch/tag/whatever.
	if e.PluginsAlwaysCloneFresh && osutil.FileExists(pluginDirectory) {
		e.shell.Commentf("BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH is true; removing previous checkout of plugin %s", p.Label())
		if err := os.RemoveAll(pluginDirectory); err != nil {
			e.shell.Errorf("Oh no, something went wrong removing %s", pluginDirectory)
			return nil, err
		}
	}

	// Does the .git directory exist? (i.e. it's already checkout out?)
	if osutil.FileExists(pluginGitDirectory) {
		// It'd be nice to show the current commit of the plugin, so
		// let's figure that out.
		headCommit, err := gitRevParseInWorkingDirectory(ctx, e.shell, pluginDirectory, "--short=7", "HEAD")
		if err != nil {
			e.shell.Commentf("Plugin %q already checked out (can't `git rev-parse HEAD` plugin git directory)", p.Label())
		} else {
			e.shell.Commentf("Plugin %q already checked out (%s)", p.Label(), strings.TrimSpace(headCommit))
		}

		// Open the plugin directory as the checkout root.
		pluginRoot, err := os.OpenRoot(pluginDirectory)
		if err != nil {
			return nil, fmt.Errorf("opening plugin directory as a root: %w", err)
		}
		runtime.AddCleanup(checkout, func(r *os.Root) { r.Close() }, pluginRoot)
		checkout.Root = pluginRoot

		// Ensure hooks is a directory that exists within the checkout.
		if fi, err := pluginRoot.Stat(checkout.HooksDir); err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("%q was not a directory within the %q plugin: %w", checkout.HooksDir, checkout.Plugin.Name(), err)
		}
		return checkout, nil
	}

	e.shell.Commentf("Plugin %q will be checked out to %q", p.DisplayName(), pluginDirectory)

	repo, err := p.Repository()
	if err != nil {
		return nil, err
	}

	if e.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(ctx, e.shell, repo)
	}

	// Make the directory. Use a random name that _doesn't_ look like a plugin
	// name, to avoid the `cd ...` line looking like it contains the final path.
	e.shell.Promptf("mktemp -dp %s", shellwords.Quote(e.PluginsPath))
	tempDir, err := os.MkdirTemp(e.PluginsPath, "")
	if err != nil {
		return nil, err
	}

	// Switch to the plugin directory
	e.shell.Commentf("Switching to the temporary plugin directory")
	previousWd := e.shell.Getwd()
	if err := e.shell.Chdir(tempDir); err != nil {
		return nil, err
	}
	// Switch back to the previous working directory
	defer func() {
		if err := e.shell.Chdir(previousWd); err != nil && e.Debug {
			e.shell.Errorf("failed to switch back to previous working directory: %v", err)
		}
	}()

	args := []string{"clone", "-v"}

	if e.GitSubmodules {
		// "--recursive" was added in Git 1.6.5, and is an alias to
		// "--recurse-submodules" from Git 2.13.
		args = append(args, "--recursive")
	}

	args = append(args, "--", repo, ".")

	// Plugin clones shouldn't use custom GitCloneFlags
	err = roko.NewRetrier(
		roko.WithMaxAttempts(3),
		roko.WithStrategy(roko.Constant(2*time.Second)),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		return e.shell.Command("git", args...).Run(ctx)
	})
	if err != nil {
		return nil, err
	}

	// Switch to the version if we need to
	if p.Version != "" {
		e.shell.Commentf("Checking out `%s`", p.Version)
		if err = e.shell.Command("git", "checkout", "-f", p.Version).Run(ctx); err != nil {
			return nil, err
		}
	}

	e.shell.Commentf("Moving temporary plugin directory to final location")
	e.shell.Promptf("mv %s %s", shellwords.Quote(tempDir), shellwords.Quote(pluginDirectory))
	err = os.Rename(tempDir, pluginDirectory)
	if err != nil {
		return nil, err
	}

	// Open the plugin directory (that we just moved into position) as the
	// checkout root.
	pluginRoot, err := os.OpenRoot(pluginDirectory)
	if err != nil {
		return nil, fmt.Errorf("opening plugin directory as a root: %w", err)
	}
	runtime.AddCleanup(checkout, func(r *os.Root) { r.Close() }, pluginRoot)
	checkout.Root = pluginRoot

	// Ensure hooks is a directory that exists within the checkout.
	if fi, err := pluginRoot.Stat(checkout.HooksDir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("%q was not a directory within the %q plugin: %w", checkout.HooksDir, checkout.Plugin.Name(), err)
	}

	return checkout, nil
}
