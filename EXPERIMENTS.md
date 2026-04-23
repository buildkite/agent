# Experiments

We frequently introduce new experimental features to the agent. You can use the `--experiment` flag to opt-in to them and test them out:

```bash
buildkite-agent start --experiment experiment1 --experiment experiment2
```

Or you can set them in your [agent configuration file](https://buildkite.com/docs/agent/v3/configuration):

```
experiment="experiment1,experiment2"
```

If an experiment doesn't exist, no error will be raised.

**Please note that there is every chance we will remove or change these experiments, so using them should be at your own risk and without the expectation that they will work in future!**

## Available Experiments

### `agent-api`

This exposes a local API for interacting with the agent process.
...with primitives that can be used to solve local concurrency problems (such as multiple agents handling some shared local resource).

The API is exposed via a Unix Domain Socket. The path to the socket is not available via a environment variable - rather, there is a single (configurable) path on the system.

**Status:** Experimental while we iron out the API and test it out in the wild. We'll probably promote this to non-experiment soon™.

### `pty-raw`

Set PTY to raw mode, to avoid mapping LF (\n) to CR,LF (\r\n) in job command output.
These extra newline characters are normally not noticed, but can make raw logs appear double-spaced
in some circumstances.

We run commands in a PTY mostly (entirely?) so that the program detects a PTY and behaves like it's
running in a terminal, using ANSI escapes to provide colours, progress meters etc. But we don't need
the PTY to modify the stream. (Or do we? That's why this is an experiment)

**Status:** Experimental for some opt-in testing before being promoted to always-on.

### `interpolation-prefers-runtime-env`

When interpolating the pipeline level environment block, a pipeline level environment variable could take precedence over environment variables depending on the ordering. This may contravene Buildkite's [documentation](https://buildkite.com/docs/pipelines/environment-variables#environment-variable-precedence) that suggests the Job runtime environment takes precedence over that defined by combining environment variables defined in a pipeline.

We previously made this the default behaviour of the agent (as of v3.63.0) but have since reverted it.

**Status:** Available as an experiment to allow users who have since depended on this behaviour to re-enable it. If you use this feature please let us know so we may better understand your use case.

### `zip-plugins`

Allows plugins to be downloaded as zip archives instead of being cloned from a Git repository. This is useful for plugins hosted as zip files on HTTP(S) URLs.

**Status:** Experimental while we test zip archive support for plugins.

### `legacy-post-hook-order`

This experiment is an escape hatch that reverts to the v3 execution order of `post-checkout` and `post-command` hooks.

In Agent v3, hooks of any kind would run in the same order as one another (for plugins, the order in which plugins are specified for a step). In v4, multiple `post-checkout`, `post-command`, or `pre-exit` hooks execute in _reverse_ order. This change makes it easier for multiple plugins and hooks to compose.

For example, suppose a step specifies two plugins A and B, and there are also agent and repository hooks. Under version 3, each hook type would execute in the same order:

- agent pre-checkout
- (pre-checkout is not possible for repository hooks)
- plugin A pre-checkout
- plugin B pre-checkout
- (checkout)
- agent post-checkout
- repository post-checkout
- plugin A post-checkout
- plugin B post-checkout
- agent pre-command
- repository pre-command
- plugin A pre-command
- plugin B pre-command
- (command)
- agent post-command
- repository post-command
- plugin A post-command
- plugin B post-command
- agent pre-exit
- repository pre-exit
- plugin A pre-exit
- plugin B pre-exit

Under version 4, the execution order is (key differences in bold):

- agent pre-checkout
- (pre-checkout is not possible for repository hooks)
- plugin A pre-checkout
- plugin B pre-checkout
- (checkout)
- plugin **B** post-checkout
- plugin **A** post-checkout
- **repository** post-checkout
- **agent** post-checkout
- agent pre-command
- repository pre-command
- plugin A pre-command
- plugin B pre-command
- (command)
- plugin **B** post-command
- plugin **A** post-command
- **repository** post-command
- **agent** post-command
- plugin **B** pre-exit
- plugin **A** pre-exit
- **repository** pre-exit
- **agent** pre-exit

**Status:** This escape-hatch experiment will be removed in a future release, once we are confident that it isn't needed.
