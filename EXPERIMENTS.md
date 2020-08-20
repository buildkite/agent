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
