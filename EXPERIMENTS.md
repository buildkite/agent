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

### `git-mirrors`

Maintain a single bare git mirror for each repository on a host that is shared amongst multiple agents and pipelines. Checkouts reference the git mirror using `git clone --reference`, as do submodules.

You must set a `git-mirrors-path` in your config for this to work.

**Status**: broadly useful, we'd like this to be the standard behaviour in 4.0. 👍👍

### `ansi-timestamps`

Outputs inline ANSI timestamps for each line of log output which enables toggle-able timestamps in the Buildkite UI.

**Status**: broadly useful, we'd like this to be the standard behaviour in 4.0. 👍👍

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

### `flock-file-locks`

Changes the file lock implementation from github.com/nightlyone/lockfile to github.com/gofrs/flock to address an issue where file locks are never released by agents that don't shut down cleanly.

When the experiment is enabled the agent will use different lock files from agents where the experiment is disabled, so agents with this experiment enabled should not be run on the same host as agents where the experiment is disabled.

**Status**: Being tested, but it's looking good. We plan to switch lock implementations over the course of a couple of releases, switching over in such a way that nothing gets broken.

### `kubernetes-exec`
Modifies `start` and `bootstrap` in such a way that they can run in separate Kubernetes containers in the same pod.

Currently, this experiment is being used by [agent-stack-k8s](https://github.com/buildkite/agent-stack-k8s).

This will result in errors unless orchestrated in a similar manner to that project. Please see the [README](https://github.com/buildkite/agent-stack-k8s/blob/main/README.md) of that repository for more details.

**Status**: Being used in a preview release of agent-stack-k8s. As it has little applicability outside of Kubernetes, this will not be the default behaviour.

### `descending-spawn-priority`

Changes the priority numbering when using `--spawn-with-priority`. By default, priorities start at 1 and increase. Using this experiment, priorities start at -1 and decrease. (Yes, negative priorities are allowed!) This experiment fixes imbalanced work assignment among different hosts with agents that have different values for `--spawn`. 

For example, without this experiment and all other things being equal, a host with `--spawn=3` would normally need to be running at least two jobs before a host with `--spawn=1` would see any work, because the two extra spawn would have higher priorities. With this experiment, one job would be running on both hosts before the additional spawn on the first host are assigned work.

**Status**: Likely to become the default in a release soon.