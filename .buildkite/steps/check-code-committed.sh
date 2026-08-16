#!/usr/bin/env sh

set -euf

export GOCACHE="$HOME/.gocache"
export GOMODCACHE="$HOME/.gomodcache"

echo --- :inbox_tray: cache restore
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

# Ensure the module cache holds the full dependency graph before saving.

# `go mod download` fetches every module's source (including deps imported
# only under other-platform build tags) so the single, platform-independent
# gomodcache key we save serves arm64 and windows restores too.
echo --- :arrow_down: go mod download
go mod download

# Writer for two keys: the single platform-independent gomodcache, and the
# linux/amd64 gocache. This step is unparallelized, so it never races itself.
echo --- :outbox_tray: cache save
buildkite-agent cache save --name gomodcache --name gocache
