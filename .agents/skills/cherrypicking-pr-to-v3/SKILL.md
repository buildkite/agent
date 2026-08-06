---
name: cherrypicking-pr-to-v3
description: Backports (cherry-picks) a merged PR from main (v4) to the v3 branch of buildkite/agent, translating v4 code back to v3 conventions. Use when asked to backport, cherry-pick, or port a PR or commit to v3.
---

# Cherry-picking a PR to v3

Since August 2026, `main` is the v4 codebase. Stable v3 releases come from the
`v3` branch. A change that needs to reach customers in v3 must first merge to
`main`, then be backported as a second PR against `v3` with the same changes
translated back to v3 conventions.

## Workflow

1. Identify the merged PR/commit on `main` to backport. Never cherry-pick the
   v4 merge commit (`8aea45a`, PR #3807) itself — it is the entire v4 delta.
2. Apply the change onto `v3`: with jj, `jj duplicate <rev> -d v3` (then
   `jj edit` the duplicate to resolve conflicts); with git, branch off `v3`
   and `git cherry-pick -x --no-commit <sha>`. Resolve conflicts by
   translating the code as described below. PRs in this repo merge as
   two-parent merge commits, and the change lives in the constituent
   commit(s), not the merge commit: `jj duplicate` of the merge commit
   produces an empty commit, and `git cherry-pick` refuses it without
   `-m 1`. Select the PR's constituent commit(s) instead.
3. Translate all v4-isms back to v3 (see checklist).
4. Raise the backport PR against `v3`, referencing the original PR.
5. Do not touch the `VERSION` file unless a release is explicitly requested.

## Translation checklist (v4 → v3)

### 1. Import paths

The module changed major version. Rewrite every import:

```diff
-"github.com/buildkite/agent/v4/api"
+"github.com/buildkite/agent/v3/api"
```

Also check string-valued package paths, not just imports: tracer names
(`otel.Tracer("github.com/buildkite/agent/...")`), linker flags, generated
protobuf `go_package_prefix`, doc links. Never change the v3 branch's
`go.mod` module declaration.

### 2. urfave/cli: v3 → v1

`main` uses `github.com/urfave/cli/v3`; the `v3` branch uses
`github.com/urfave/cli` (v1). Flags and commands are pointers in cli v3 but
values in cli v1, and env vars are declared differently:

```go
// main (cli v3)
&cli.StringFlag{
    Name:    "note",
    Usage:   "A descriptive note to record why the agent is paused",
    Sources: cli.EnvVars("BUILDKITE_AGENT_PAUSE_NOTE"),
}

// v3 branch (cli v1)
cli.StringFlag{
    Name:   "note",
    Usage:  "A descriptive note to record why the agent is paused",
    EnvVar: "BUILDKITE_AGENT_PAUSE_NOTE",
}
```

| main (cli v3) | v3 branch (cli v1) |
|---|---|
| `import "github.com/urfave/cli/v3"` | `import "github.com/urfave/cli"` |
| `&cli.Command{...}` | `cli.Command{...}` |
| `[]*cli.Command` | `[]cli.Command` |
| `&cli.StringFlag{...}` (and Bool/Int/etc.) | `cli.StringFlag{...}` |
| `Sources: cli.EnvVars("X")` | `EnvVar: "X"` |
| `Commands: []*cli.Command{...}` (subcommands) | `Subcommands: []cli.Command{...}` |
| `StringSliceFlag{Value: []string{}}` | `StringSliceFlag{Value: &cli.StringSlice{}}` |
| `cli.Exit(...)` | `cli.NewExitError(...)` |
| `flag.Names()` | `flag.GetName()` |

### 3. Action signatures and context

cli v3 actions receive a `context.Context`; cli v1 actions do not:

```go
// main (cli v3)
Action: func(ctx context.Context, c *cli.Command) error {
    ctx, cfg, l, _, done := setupLoggerAndConfig[Config](ctx, c)
    ...
}

// v3 branch (cli v1)
Action: func(c *cli.Context) error {
    ctx := context.Background()
    ctx, cfg, l, _, done := setupLoggerAndConfig[Config](ctx, c)
    ...
}
```

Named action functions translate the same way:
`func fooAction(ctx context.Context, c *cli.Command) error` becomes
`func fooAction(c *cli.Context) error` with `ctx := context.Background()`
inside. Do not change v3's shared setup helpers to accept `*cli.Command`.

### 4. CLI accessors

| main (cli v3) | v3 branch (cli v1) |
|---|---|
| `c *cli.Command` | `c *cli.Context` |
| `c.Root().Writer` / `c.Root().ErrWriter` | `c.App.Writer` / `c.App.ErrWriter` |
| `c.Args().Len()` | `len(c.Args())` |
| `c.Args().Slice()` | `c.Args()` |
| `c.Args().Get(0)` | `c.Args()[0]` |
| `clicommand.SomeFlag.Value` (slice flag, `[]string`) | `*clicommand.SomeFlag.Value` (deref `*cli.StringSlice`) |

### 5. Tests

cli v3 tests build a root `*cli.Command`, clone commands (Run mutates them),
and call `app.Run(t.Context(), args)`. v3 tests use the v1 harness:

```go
app := cli.NewApp()
app.Commands = []cli.Command{SomeCommand}
err := app.Run(args)
```

Do not copy the cli v3 command-cloning workaround into v3 tests.

## Behavioral differences — don't backport v4 breaks by accident

A conflict may signal a deliberate v4 behavior change, not just code drift.
Preserve v3 behavior unless changing it is the explicit purpose of the
backport:

- **Tracing/metrics**: v3 still has Datadog/OpenTracing and DogStatsD
  (`TracingBackend`, `TracingServiceName`); v4 is OpenTelemetry-only
  (`OpenTelemetryTracing`, `TelemetryServiceName`). Map config fields back.
- **Cancellation flags**: v3 has `--cancel-grace-period` and
  `--signal-grace-period-seconds`; v4 replaced them with
  `--cancel-signal-timeout` and `--cancel-cleanup-timeout`.
- **Experiments**: v4 promoted or removed several experiments (normalized
  artifact upload paths, resolve-commit-after-checkout, etc.). A v4 diff may
  lack an experiment guard because the behavior is unconditional there; the
  v3 backport may need to keep or add the guard.
- **Hook ordering**: v4 reverses post-hook order by default; v3 does not.
- **Commit verification**: v4 defaults to `strict` and only supports
  `strict`/`off`; v3 supports the older empty/`warn`/`strict` values.
- **SSH host keys**: v4 uses `GIT_SSH_COMMAND` with
  `StrictHostKeyChecking=accept-new`; v3 still uses `ssh-keyscan` and
  `known_hosts`.
- **Removed in v4, still in v3**: deprecated Docker integration, deprecated
  artifact flags (`follow-symlinks`), plugin env aliases, header-times
  streamer/API. If the v4 patch assumes these are gone, the v3 backport
  needs a v3-specific integration point.
- **Output**: v4 intentionally changed some output (e.g. `meta-data get`
  gained a trailing newline). Don't pull such compatibility breaks into v3.

## Reference

- Changelog: https://buildkite.com/resources/changelog/382-buildkite-agent-v4-becomes-the-stable-release-on-1-september-2026/
- Notion: "Making agent changes in the run-up to v4"
  (buildkite Notion, page `3b3b8dbc2c8980b5a623e6e678da893b`)
- The v4 merge to main: https://github.com/buildkite/agent/pull/3807
