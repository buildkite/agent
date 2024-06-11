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

### `override-zero-exit-on-cancel`

If the job is cancelled, and the exit status of the process is 0, it is overridden to be 1 instead.

When cancelling a job, the agent signals the process, which typically causes it to exit with a
non-zero status code. On Windows this is not true - the process exits with code 0 instead, which
makes the job appear to be successful. (It successfully exited, no?) By overriding the status to 1,
a cancelled job should appear as a failure, regardless of the OS the agent is running on.

**Status:** Experimental for some opt-in testing. We hope to promote this to be the default soon™.

### `interpolation-prefers-runtime-env`

When interpolating the pipeline level environment block, a pipeline level environment variable could take precedence over environment variables depending on the ordering. This may contravene Buildkite's [documentation](https://buildkite.com/docs/pipelines/environment-variables#environment-variable-precedence) that suggests the Job runtime environment takes precedence over that defined by combining environment variables defined in a pipeline. 

We previously made this the default behaviour of the agent (as of v3.63.0) but have since reverted it.

**Status:** Available as an experiment to allow users who have since depended on this behaviour to re-enable it. If you use this feature please let us know so we may better understand your use case.

### `allow-artifact-path-traversal`

Uploaded artifacts include a relative path used by the artifact downloader to download the artifact to a suitable location relative to the destination path. In most circumstances the relative paths generated by `artifact upload` won't contain `..` components, and so will always be downloaded at or inside the destination path.

However, it is possible to upload artifacts using glob patterns containing one or more `..` components, which may be preserved in the artifact path. It is also possible for a user to call the Agent REST API directly in order to upload artifacts with arbitrary paths.

Leaving this experiment disabled prevents `..` components in artifact paths from traversing up from the destination path. Enabling this experiment permits the less-secure behaviour of allowing artifact paths containing `..` to traverse up the destination path.

For example, if an artifact was uploaded with the path `../../foo.txt`, then the command:

```shell
buildkite-agent artifact download '*.txt' .
```

has a different effect depending on this experiment:

- With `allow-artifact-path-traversal` disabled, `foo.txt` is downloaded to `./foo.txt`.
- With `allow-artifact-path-traversal` enabled, `foo.txt` is downloaded to `../../foo.txt`.

**Status:** This experiment is an escape hatch for a security fix. While the new behaviour is more secure, it may break downloading of legitimately-uploaded artifacts.
