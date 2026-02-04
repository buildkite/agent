#!/usr/bin/env sh

set -euf

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
  # While we're cleaning up things found by golangci-lint, don't fail if it
  # finds things.
  exit 0
fi

echo +++ Everything is clean and tidy! 🎉
