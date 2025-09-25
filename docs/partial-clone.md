# Partial Clone and Sparse Checkout

The Buildkite Agent supports partial clones and sparse checkouts, which can significantly reduce clone times and disk space usage for large repositories.

## Overview

Partial clones allow you to clone a repository without downloading all of its history or objects, while sparse checkout allows you to check out only specific directories or files from a repository.

## Configuration

### Environment Variables

The following environment variables control partial clone behavior:

- `BUILDKITE_GIT_SPARSE_CHECKOUT` - Enable sparse checkout (boolean: `true` or `false`)
- `BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS` - Comma-separated list of paths to include in sparse checkout
- `BUILDKITE_GIT_CLONE_DEPTH` - Clone depth for shallow clones (e.g., `200`)
- `BUILDKITE_GIT_CLONE_FILTER` - Filter specification for partial clones (e.g., `tree:0`)

### Command Line Flags

When starting an agent, you can also use command line flags:

```bash
buildkite-agent start \
  --git-sparse-checkout \
  --git-sparse-checkout-paths "src/frontend,src/backend" \
  --git-clone-depth 200 \
  --git-clone-filter "tree:0"
```

## Examples

### Example 1: Sparse Checkout with Multiple Directories

To check out only specific directories from a monorepo:

```yaml
env:
  BUILDKITE_GIT_SPARSE_CHECKOUT: "true"
  BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS: "services/api,services/web,shared/utils"
```

### Example 2: Shallow Clone with Partial Objects

For a shallow clone with limited history and filtered objects:

```yaml
env:
  BUILDKITE_GIT_CLONE_DEPTH: "100"
  BUILDKITE_GIT_CLONE_FILTER: "blob:none"
```

### Example 3: Complete Partial Clone Setup

Combining all features for maximum optimization:

```yaml
env:
  BUILDKITE_GIT_CLONE_DEPTH: "200"
  BUILDKITE_GIT_CLONE_FILTER: "tree:0"
  BUILDKITE_GIT_SPARSE_CHECKOUT: "true"
  BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS: "my-service"
```

This configuration will:
1. Clone only the last 200 commits
2. Exclude tree objects that aren't needed (`tree:0`)
3. Only check out the `my-service` directory

## Git Clone Filters

The `BUILDKITE_GIT_CLONE_FILTER` supports various filter specifications:

- `blob:none` - Omit all blob objects (file contents)
- `blob:limit=<size>` - Omit blobs larger than `<size>` bytes
- `tree:0` - Omit all tree objects (directory listings)
- `tree:<depth>` - Omit tree objects at specified depth

## Sparse Checkout Paths

The `BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS` variable accepts:
- Single directory: `"src/frontend"`
- Multiple directories: `"src/frontend,src/backend,docs"`
- Paths with wildcards are not supported in cone mode (which is the default)

## Performance Considerations

1. **Network Usage**: Partial clones significantly reduce network bandwidth usage
2. **Disk Space**: Sparse checkouts reduce local disk space usage
3. **Clone Time**: Both features can dramatically reduce initial clone times
4. **Fetch Time**: Subsequent fetches will only download required objects

## Compatibility

- Requires Git 2.25.0 or later for partial clone support
- Requires Git 2.25.0 or later for cone-mode sparse checkout
- The repository must be hosted on a server that supports partial clones

## Job-to-Job Behavior

When using sparse checkout, the agent automatically handles transitions between jobs:

- If a job uses sparse checkout and the next job doesn't, sparse checkout is automatically disabled
- If a job doesn't use sparse checkout and the next job does, sparse checkout is automatically enabled
- This ensures each job gets the correct view of the repository without manual intervention

## Troubleshooting

1. **Missing objects**: If you encounter "object not found" errors, you may need to adjust your filter settings or disable partial clones
2. **Sparse checkout not working**: Ensure paths are comma-separated and don't contain spaces
3. **Performance issues**: Some operations may trigger on-demand object downloads; monitor your builds to ensure partial clones provide the expected benefits