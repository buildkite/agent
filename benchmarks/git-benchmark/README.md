# Git Checkout Benchmark

## Goal

Measure the performance of the current Buildkite agent checkout paths under realistic bootstrap workloads.

The first version of this suite is intentionally limited to the checkout strategies that already exist in the agent today:

1. direct checkout with no local Git cache
2. direct shallow checkout with `--depth=1`
3. direct blobless checkout with `--filter=blob:none`
4. checkout with `BUILDKITE_GIT_MIRRORS_PATH` in `reference` mode
5. checkout with `BUILDKITE_GIT_MIRROR_CHECKOUT_MODE=dissociate`

The benchmark should answer four practical questions:

1. How much faster are mirror-backed checkouts than direct checkouts on warm hosts?
2. How much upstream traffic do mirrors avoid once primed?
3. What happens under concurrent checkout load on a single host?
4. What is the cold-host cost of using mirrors at all?

## What We Are Comparing

Benchmark the real `buildkite-agent bootstrap --phases checkout` path, not isolated `git clone` commands.

### Variant A: direct clone control

- no mirrors
- normal checkout path against the upstream remote

This is the control group.

### Variant B: direct shallow clone control

- no mirrors
- `BUILDKITE_GIT_CLONE_FLAGS=-v --depth=1`
- `BUILDKITE_GIT_FETCH_FLAGS=-v --prune --depth=1`

This measures the trade-off of a shallow direct checkout.

### Variant C: direct blobless clone control

- no mirrors
- `BUILDKITE_GIT_CLONE_FLAGS=-v --filter=blob:none`
- `BUILDKITE_GIT_FETCH_FLAGS=-v --prune --filter=blob:none`

This measures a direct partial clone that defers historical blob transfer.

### Variant D: existing mirror checkout in `reference` mode

- `BUILDKITE_GIT_MIRRORS_PATH` enabled
- `BUILDKITE_GIT_MIRROR_CHECKOUT_MODE=reference`

This exercises the current fastest mirror-backed path.

### Variant E: existing mirror checkout in `dissociate` mode

- `BUILDKITE_GIT_MIRRORS_PATH` enabled
- `BUILDKITE_GIT_MIRROR_CHECKOUT_MODE=dissociate`

This measures the safer mirror mode that avoids long-term object borrowing from the mirror.

## Scope

This suite is for the current shipped checkout implementation only.

It measures:

- checkout latency
- upstream requests and bytes transferred
- warm-host reuse of mirrors
- cold-host mirror setup cost
- concurrent behaviour on a single host
- rough disk consumption of the mirror directory

It does not attempt to measure:

- daemon-based Git caching
- treeless partial clone
- Git LFS behaviour
- submodule acceleration
- plugin-managed checkout flows
- cross-host or remote cache behaviour

## Repositories

Use three repositories with different purposes.

### 1. `buildkite/agent`

Use this as a harness sanity check.

- fast to iterate on
- easy to debug when the driver is wrong
- too small to be the headline result

### 2. `rails/rails`

Use this as the primary benchmark repository.

- large enough to make checkout costs real
- common and recognisable
- still practical to run repeatedly

### 3. `kubernetes/kubernetes`

Use this as the large stress case once the harness is stable.

## Upstream Setup

Do not benchmark against live GitHub.

Instead:

1. mirror the source repository into a controlled upstream bare repository
2. serve that repository from a local or nearby Git daemon
3. optionally place Toxiproxy in front of it to apply a stable network profile

The harness already does this by creating a local bare upstream, serving it with `git daemon`, and measuring traffic through a counted TCP proxy.

## Workloads

Start with the smallest matrix that still reflects real checkout behaviour.

### 1. Cold host, single checkout

- empty mirror directory
- empty checkout directory
- one bootstrap process

Purpose:

- measure first-hit cost
- compare mirror setup against direct checkout

### 2. Warm host, single checkout

- mirror already primed when applicable
- fresh checkout directory per run
- one bootstrap process

Purpose:

- measure steady-state warm performance

### 3. Warm host, concurrent checkouts of the same commit

- mirror already primed when applicable
- multiple bootstrap processes target the same commit at once

Purpose:

- measure host contention and reuse under concurrency

### 4. Warm host, concurrent checkouts of a new commit

- mirror primed first
- one new upstream commit created before the timed round
- multiple bootstrap processes target that new commit at once

Purpose:

- measure how well the current mirror implementation handles a shared miss

## Running The Harness

From the repository root, the canonical entrypoint is the `mise` task:

```bash
mise run git-benchmark
```

By default the task builds the current agent binary and then runs the harness.

Unless `--keep-workdir` is set, the harness cleans up transient checkout artefacts after the run while preserving the final JSON report.

Helpful invocations:

```bash
mise run git-benchmark --help
```

```bash
mise run git-benchmark \
  --source-repo https://github.com/rails/rails.git \
  --iterations 3 \
  --concurrency 8
```

## Metrics To Keep

Each report should keep:

- total worker duration
- total round duration
- upstream request count
- upstream bytes transferred
- per-command timings from Git trace2 for clone, fetch, checkout, and clean
- final mirror size on disk
- exact agent and Git versions used

## Minimum Useful Report

A useful first report should include:

- `rails/rails`
- `direct`
- `direct-shallow`
- `direct-blobless`
- `mirror-reference`
- `mirror-dissociate`
- cold single
- warm single
- warm concurrent
- warm concurrent new commit

## Notes

This suite is deliberately a benchmark harness, not a product proposal.

It is meant to answer how the current direct and mirror-based checkout paths behave today, so future checkout changes can be compared against a repeatable baseline.
