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

### `descending-spawn-priority`

When using `--spawn` with `--spawn-with-priority`, the agent assigns ascending priorities to each spawned agent (1, 2, 3, ...). This experiment changes the priorities to be descending (-1, -2, -3, ...) instead. This helps jobs be assigned across all hosts in cases where the value of `--spawn` varies between hosts.

**Status:** Experimental as an escape hatch to default behaviour. Will soon be promoted to a regular flag.
