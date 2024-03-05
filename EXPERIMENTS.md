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

### `normalised-upload-paths`

Artifacts found by `buildkite-agent artifact upload` will be uploaded using URI/Unix-style paths, even on Windows. This changes the URLs that artifacts uploaded from Windows agents are stored at, but to one which is URI-compatible.

Artifact names displayed in Buildkite's web UI, as well as in the API, are changed by this.

Take `buildkite-agent artifact upload coverage\report.xml` as an example:

- By default, and without this experiment, this file is uploaded to `s3://example/coverage\report.xml`.
- With this experiment enabled, it would be `s3://example/coverage/report.xml`.

**Status**: a major improvement for Windows compatibility, we'd like this to be the standard behaviour in 4.0. 👍👍

### `resolve-commit-after-checkout`

After repository checkout, resolve `BUILDKITE_COMMIT` to a commit hash. This makes `BUILDKITE_COMMIT` useful for builds triggered against non-commit-hash refs such as `HEAD`.

**Status**: broadly useful, we'd like this to be the standard behaviour in 4.0. 👍👍

### `kubernetes-exec`

Modifies `start` and `bootstrap` in such a way that they can run in separate Kubernetes containers in the same pod.

Currently, this experiment is being used by [agent-stack-k8s](https://github.com/buildkite/agent-stack-k8s).

This will result in errors unless orchestrated in a similar manner to that project. Please see the [README](https://github.com/buildkite/agent-stack-k8s/blob/main/README.md) of that repository for more details.

**Status**: Being used in a preview release of agent-stack-k8s. As it has little applicability outside of Kubernetes, this will not be the default behaviour.

### `polyglot-hooks`

Allows the agent to run hooks written in languages other than bash. This enables the agent to run hooks written in any language, as long as the language has a runtime available on the agent. Polyglot hooks can be in interpreted languages, so long as they have a valid shebang, and the interpreter specified in the shebang is installed on the agent.

This experiment also allows the agent to run compiled binaries (such as those produced by Go, Rust, Zig, C et al.) as hooks, so long as they are executable.

Hooks are run in a subshell, so they can't modify the environment of the agent process. However, they can use the [job-api](#job-api) to modify the environment of the job.

Binary hooks are available on all platforms, but interpreted hooks are unfortunately unavailable on Windows, as Windows does not support shebangs.

**Status:** Experimental while we try to cover the various corner cases. We'll probably promote this to non-experiment soon™️.

### `agent-api`

This exposes a local API for interacting with the agent process.
...with primitives that can be used to solve local concurrency problems (such as multiple agents handling some shared local resource).

The API is exposed via a Unix Domain Socket. The path to the socket is not available via a environment variable - rather, there is a single (configurable) path on the system.

**Status:** Experimental while we iron out the API and test it out in the wild. We'll probably promote this to non-experiment soon™.

### `isolated-plugin-checkout`

Checks out each plugin to an directory that that is namespaced by the agent name. Thus each agent worker will have an isolated copy of the plugin. This removes the need to lock the plugin checkout directories in a single agent process with spawned workers. However, if your plugin directory is shared between multiple agent processes *with the same agent name* , you may run into race conditions. This is just one reason we recommend you ensure agents have unique names.

**Status:** Likely to be the default behaviour in the future.

### `use-zzglob`

Uses a different library for resolving glob expressions used for `artifact upload`.
The new glob library should resolve a few issues experienced with the old library:

- Because `**` is used to mean "zero or more path segments", `/**/` should match `/`.
- Directories that cannot match the glob pattern shouldn't be walked while resolving the pattern. Failure to do this makes `artifact upload` difficult to use when run in a directory containing a mix of subdirectories with different permissions.
- Failures to walk potential file paths should be reported individually.

The new library should handle all syntax supported by the old library, but because of the chance of incompatibilities and bugs, we're providing it via experiment only for now.

**Status:** Since using the old library causes problems, we hope to promote this to be the default soon™️.

### `pty-raw`

Set PTY to raw mode, to avoid mapping LF (\n) to CR,LF (\r\n) in job command output.
These extra newline characters are normally not noticed, but can make raw logs appear double-spaced
in some circumstances.

We run commands in a PTY mostly (entirely?) so that the program detects a PTY and behaves like it's
running in a terminal, using ANSI escapes to provide colours, progress meters etc. But we don't need
the PTY to modify the stream. (Or do we? That's why this is an experiment)

**Status:** Experimental for some opt-in testing before being promoted to always-on.
