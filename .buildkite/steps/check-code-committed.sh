#!/usr/bin/env sh

set -euf

export GOCACHE="$HOME/.gocache"
export GOMODCACHE="$HOME/.gomodcache"
export AGENT_GO_VERSION="$(go env GOVERSION | cut -d. -f1,2)"

echo --- :inbox_tray: Restoring Go caches
buildkite-agent cache restore --name gomodcache --name gocache

echo --- :go: Checking go mod tidyness
go mod tidy
if ! git diff --no-ext-diff --exit-code; then
  echo ^^^ +++
  echo "The go.mod or go.sum files are out of sync with the source code"
  echo "Please run \`go mod tidy\` locally, and commit the result."

  exit 1
fi

echo +++ :go: Checking go formatting

fumpt_out=$(go tool gofumpt -extra -l .)
if ! [ -z "${fumpt_out}" ]; then
  echo ^^^ +++
  echo "Files have not been formatted with gofumpt:"
  echo "${fumpt_out}"
  echo "Fix this by running \`gofumpt -extra -w .\` locally, and committing the result."

  exit 1
fi

echo --- :go: Generating code
go generate ./...
if ! git diff --no-ext-diff --exit-code; then
  echo ^^^ +++
  echo :x: Generated code was not commited.
  echo "Run"
  echo "  go generate ./..."
  echo "and make a commit."

  exit 1
fi

echo --- :go: Running assertzapper...
if ! lint_out="$(go tool assertzapper ./...)" ; then
  echo ^^^ +++
  echo "assertzapper found uses of an assert library:"
  echo ""
  echo "${lint_out}"
  echo "Run"
  echo "  go tool assertzapper -fix ./..."
  echo "then refine any changes it makes, and make a commit."
  exit 1
fi

echo +++ :go: Running golangci-lint...
if ! lint_out="$(golangci-lint run --color=always)" ; then
  echo ^^^ +++
  echo "golangci-lint found the following issues:"
  echo ""
  echo "${lint_out}"
  buildkite-agent annotate --style=warning <<EOF
golangci-lint found the following issues:

\`\`\`term
${lint_out}
\`\`\`
EOF
  exit 1
fi

echo +++ Everything is clean and tidy! 🎉

# Populate the shared module cache with the complete dependency graph before
# this step saves it for jobs on every platform.
echo --- :arrow_down: Downloading Go modules
go mod download

# This unsharded step owns the shared module cache and Linux AMD64 build cache,
# avoiding competing saves from the parallel test jobs.
echo --- :outbox_tray: Saving Go caches
buildkite-agent cache save --name gomodcache --name gocache
