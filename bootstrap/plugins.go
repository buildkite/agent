package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/agent/plugin"
	"github.com/buildkite/agent/v3/hook"
	"github.com/buildkite/agent/v3/utils"
	"github.com/buildkite/roko"
	"github.com/pkg/errors"
)

type pluginCheckout struct {
	*plugin.Plugin
	*plugin.Definition
	CheckoutDir string
	HooksDir    string
}

func (b *Bootstrap) hasPlugins() bool {
	return b.Config.Plugins != ""
}

func (b *Bootstrap) preparePlugins() error {
	if !b.hasPlugins() {
		return nil
	}

	b.shell.Headerf("Preparing plugins")

	if b.Debug {
		b.shell.Commentf("Plugin JSON is %s", b.Plugins)
	}

	// Check if we can run plugins (disabled via --no-plugins)
	if !b.Config.PluginsEnabled {
		if !b.Config.LocalHooksEnabled {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-local-hooks`")
		} else if !b.Config.CommandEval {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-command-eval`")
		} else {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-plugins`")
		}
	}

	var err error
	b.plugins, err = plugin.CreateFromJSON(b.Config.Plugins)
	if err != nil {
		return errors.Wrap(err, "Failed to parse a plugin definition")
	}

	if b.Debug {
		b.shell.Commentf("Parsed %d plugins", len(b.plugins))
	}

	return nil
}

func (b *Bootstrap) validatePluginCheckout(checkout *pluginCheckout) error {
	if !b.Config.PluginValidation {
		return nil
	}

	if checkout.Definition == nil {
		if b.Debug {
			b.shell.Commentf("Parsing plugin definition for %s from %s", checkout.Plugin.Name(), checkout.CheckoutDir)
		}

		// parse the plugin definition from the plugin checkout dir
		var err error
		checkout.Definition, err = plugin.LoadDefinitionFromDir(checkout.CheckoutDir)

		if err == plugin.ErrDefinitionNotFound {
			b.shell.Warningf("Failed to find plugin definition for plugin %s", checkout.Plugin.Name())
			return nil
		} else if err != nil {
			return err
		}
	}

	val := &plugin.Validator{}
	result := val.Validate(checkout.Definition, checkout.Plugin.Configuration)

	if !result.Valid() {
		b.shell.Headerf("Plugin validation failed for %q", checkout.Plugin.Name())
		json, _ := json.Marshal(checkout.Plugin.Configuration)
		b.shell.Commentf("Plugin configuration JSON is %s", json)
		return result
	}

	b.shell.Commentf("Valid plugin configuration for %q", checkout.Plugin.Name())
	return nil
}

// PluginPhase is where plugins that weren't filtered in the Environment phase are
// checked out and made available to later phases
func (b *Bootstrap) PluginPhase(ctx context.Context) error {
	if len(b.plugins) == 0 {
		if b.Debug {
			b.shell.Commentf("Skipping plugin phase")
		}
		return nil
	}

	checkouts := []*pluginCheckout{}

	// Checkout and validate plugins that aren't vendored
	for _, p := range b.plugins {
		if p.Vendored {
			if b.Debug {
				b.shell.Commentf("Skipping vendored plugin %s", p.Name())
			}
			continue
		}

		checkout, err := b.checkoutPlugin(p)
		if err != nil {
			return errors.Wrapf(err, "Failed to checkout plugin %s", p.Name())
		}

		err = b.validatePluginCheckout(checkout)
		if err != nil {
			return err
		}

		checkouts = append(checkouts, checkout)
	}

	// Store the checkouts for future use
	b.pluginCheckouts = checkouts

	// Now we can run plugin environment hooks too
	return b.executePluginHook(ctx, "environment", checkouts)
}

// VendoredPluginPhase is where plugins that are included in the
// checked out code are added
func (b *Bootstrap) VendoredPluginPhase(ctx context.Context) error {
	if !b.hasPlugins() {
		return nil
	}

	vendoredCheckouts := []*pluginCheckout{}

	// Validate vendored plugins
	for _, p := range b.plugins {
		if !p.Vendored {
			continue
		}

		checkoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

		pluginLocation, err := filepath.Abs(filepath.Join(checkoutPath, p.Location))
		if err != nil {
			return errors.Wrapf(err, "Failed to resolve vendored plugin path for plugin %s", p.Name())
		}

		if !utils.FileExists(pluginLocation) {
			return fmt.Errorf("Vendored plugin path %s doesn't exist", p.Location)
		}

		checkout := &pluginCheckout{
			Plugin:      p,
			CheckoutDir: pluginLocation,
			HooksDir:    filepath.Join(pluginLocation, "hooks"),
		}

		// Also make sure that plugin is within this repository
		// checkout and isn't elsewhere on the system.
		if !strings.HasPrefix(pluginLocation, checkoutPath+string(os.PathSeparator)) {
			return fmt.Errorf("Vendored plugin paths must be within the checked-out repository")
		}

		err = b.validatePluginCheckout(checkout)
		if err != nil {
			return err
		}

		vendoredCheckouts = append(vendoredCheckouts, checkout)
	}

	// Finally append our vendored checkouts to the rest for subsequent hooks
	b.pluginCheckouts = append(b.pluginCheckouts, vendoredCheckouts...)

	// Now we can run plugin environment hooks too
	return b.executePluginHook(ctx, "environment", vendoredCheckouts)
}

// Executes a named hook on plugins that have it
func (b *Bootstrap) executePluginHook(ctx context.Context, name string, checkouts []*pluginCheckout) error {
	for _, p := range checkouts {
		hookPath, err := hook.Find(p.HooksDir, name)
		if errors.Is(err, os.ErrNotExist) {
			continue // this plugin does not implement this hook
		} else if err != nil {
			return err
		}

		env, _ := p.ConfigurationToEnvironment()
		err = b.executeHook(ctx, HookConfig{
			Scope: "plugin",
			Name:  p.Plugin.Name() + " " + name,
			Path:  hookPath,
			Env:   env,
			SpanAttributes: map[string]string{
				"plugin.name":        p.Plugin.Name(),
				"plugin.version":     p.Plugin.Version,
				"plugin.location":    p.Plugin.Location,
				"plugin.is_vendored": strconv.FormatBool(p.Vendored),
			},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// If any plugin has a hook by this name
func (b *Bootstrap) hasPluginHook(name string) bool {
	for _, p := range b.pluginCheckouts {
		if _, err := hook.Find(p.HooksDir, name); err == nil {
			return true
		}
	}
	return false
}

// Checkout a given plugin to the plugins directory and return that directory
func (b *Bootstrap) checkoutPlugin(p *plugin.Plugin) (*pluginCheckout, error) {
	// Make sure we have a plugin path before trying to do anything
	if b.PluginsPath == "" {
		return nil, fmt.Errorf("Can't checkout plugin without a `plugins-path`")
	}

	// Get the identifer for the plugin
	id, err := p.Identifier()
	if err != nil {
		return nil, err
	}

	// Ensure the plugin directory exists, otherwise we can't create the lock
	// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
	err = os.MkdirAll(b.PluginsPath, 0777)
	if err != nil {
		return nil, err
	}

	// Create a path to the plugin
	pluginDirectory := filepath.Join(b.PluginsPath, id)
	pluginGitDirectory := filepath.Join(pluginDirectory, ".git")
	checkout := &pluginCheckout{
		Plugin:      p,
		CheckoutDir: pluginDirectory,
		HooksDir:    filepath.Join(pluginDirectory, "hooks"),
	}

	// Try and lock this particular plugin while we check it out (we create
	// the file outside of the plugin directory so git clone doesn't have
	// a cry about the directory not being empty)
	pluginCheckoutHook, err := b.shell.LockFile(filepath.Join(b.PluginsPath, id+".lock"), time.Minute*5)
	if err != nil {
		return nil, err
	}
	defer pluginCheckoutHook.Unlock()

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
	if b.Config.PluginsAlwaysCloneFresh && utils.FileExists(pluginDirectory) {
		b.shell.Commentf("BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH is true; removing previous checkout of plugin %s", p.Label())
		err = os.RemoveAll(pluginDirectory)
		if err != nil {
			b.shell.Errorf("Oh no, something went wrong removing %s", pluginDirectory)
			return nil, err
		}
	}

	if utils.FileExists(pluginGitDirectory) {
		// It'd be nice to show the current commit of the plugin, so
		// let's figure that out.
		headCommit, err := gitRevParseInWorkingDirectory(b.shell, pluginDirectory, "--short=7", "HEAD")
		if err != nil {
			b.shell.Commentf("Plugin %q already checked out (can't `git rev-parse HEAD` plugin git directory)", p.Label())
		} else {
			b.shell.Commentf("Plugin %q already checked out (%s)", p.Label(), strings.TrimSpace(headCommit))
		}

		return checkout, nil
	}

	b.shell.Commentf("Plugin \"%s\" will be checked out to \"%s\"", p.Location, pluginDirectory)

	repo, err := p.Repository()
	if err != nil {
		return nil, err
	}

	if b.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(b.shell, repo)
	}

	// Make the directory
	tempDir, err := ioutil.TempDir(b.PluginsPath, id)
	if err != nil {
		return nil, err
	}

	// Switch to the plugin directory
	b.shell.Commentf("Switching to the temporary plugin directory")
	previousWd := b.shell.Getwd()
	if err = b.shell.Chdir(tempDir); err != nil {
		return nil, err
	}
	// Switch back to the previous working directory
	defer b.shell.Chdir(previousWd)

	// Plugin clones shouldn't use custom GitCloneFlags
	err = roko.NewRetrier(
		roko.WithMaxAttempts(3),
		roko.WithStrategy(roko.Constant(2*time.Second)),
	).Do(func(r *roko.Retrier) error {
		return b.shell.Run("git", "clone", "-v", "--", repo, ".")
	})
	if err != nil {
		return nil, err
	}

	// Switch to the version if we need to
	if p.Version != "" {
		b.shell.Commentf("Checking out `%s`", p.Version)
		if err = b.shell.Run("git", "checkout", "-f", p.Version); err != nil {
			return nil, err
		}
	}

	b.shell.Commentf("Moving temporary plugin directory to final location")
	err = os.Rename(tempDir, pluginDirectory)
	if err != nil {
		return nil, err
	}

	return checkout, nil
}
