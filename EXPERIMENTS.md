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

### `job-api`

Exposes a local API for the currently running job to introspect and mutate its state in the form of environment variables. This allows you to write scripts, hooks and plugins in languages other than bash, using them to interact with the agent.

The API is exposed via a Unix Domain Socket, whose path is exposed to running jobs with the `BUILDKITE_AGENT_JOB_API_SOCKET` envar, and authenticated with a token exposed using the `BUILDKITE_AGENT_JOB_API_TOKEN` envar, using the `Bearer` HTTP Authorization scheme.

The API exposes the following endpoints:
- `GET /api/current-job/v0/env` - returns a JSON object of all environment variables for the current job
- `PATCH /api/current-job/v0/env` - accepts a JSON object of environment variables to set for the current job
- `DELETE /api/current-job/v0/env` - accepts a JSON array of environment variable names to unset for the current job

See [jobapi/payloads.go](./jobapi/payloads.go) for the full API request/response definitions.

The Job API is unavailable on windows agents running versions of windows prior to build 17063, as this was when windows added Unix Domain Socket support. Using this experiment on such agents will output a warning, and the API will be unavailable.

**Status:** Experimental while we iron out the API and test it out in the wild. We'll probably promote this to non-experiment soon™️.

### `polyglot-hooks`

Allows the agent to run hooks written in languages other than bash. This enables the agent to run hooks written in any language, as long as the language has a runtime available on the agent. Polyglot hooks can be in interpreted languages, so long as they have a valid shebang, and the interpreter specified in the shebang is installed on the agent.

This experiment also allows the agent to run compiled binaries (such as those produced by Go, Rust, Zig, C et al.) as hooks, so long as they are executable.

Hooks are run in a subshell, so they can't modify the environment of the agent process. However, they can use the [job-api](#job-api) to modify the environment of the job.

Binary hooks are available on all platforms, but interpreted hooks are unfortunately unavailable on Windows, as Windows does not support shebangs.

**Status:** Experimental while we try to cover the various corner cases. We'll probably promote this to non-experiment soon™️.

### `agent-api`

Like `job-api`, this exposes a local API for interacting with the agent process.
...with primitives that can be used to solve local concurrency problems (such as multiple agents handling some shared local resource).

The API is exposed via a Unix Domain Socket. Unlike the `job-api`, the path to the socket is not available via a environment variable - rather, there is a single (configurable) path on the system.

**Status:** Experimental while we iron out the API and test it out in the wild. We'll probably promote this to non-experiment soon™.

### `avoid-recursive-trap`

Some jobs are run as a bash script of the form:

```shell
trap "kill -- $$" INT TERM QUIT; <command>
```

We now understand this causes a bug, and we want to avoid it. Enabling this experiment removes the `trap` that surrounds non-script commands.

https://github.com/buildkite/agent/blob/40b8a5f3794b04bd64da6e2527857add849a35bd/internal/job/executor.go#L1980-L1993

**Status:** Since the default behaviour is buggy, we will be promoting this to non-experiment soon™️.
