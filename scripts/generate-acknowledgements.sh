#!/usr/bin/env bash
set -euo pipefail

echo 'Generating OSS attributions'

# Note that go-licenses output can vary by GOOS and GOARCH.
# https://github.com/google/go-licenses/issues/187
echo "GOOS=${GOOS:-not set}"
echo "GOARCH=${GOARCH:-not set}"

cd "$(git rev-parse --show-toplevel)"

if [[ ! -f "./go.mod" ]]; then
    echo "Couldn't find go.mod - are you in the agent repository?"
    exit 1
fi

# Ensure modules are downloaded
go mod download

# Get go-licenses tool
if ! command -v go-licenses >/dev/null; then
	go install github.com/google/go-licenses@latest
	GO_LICENSES="$(go env GOPATH)/bin/go-licenses"
else
	GO_LICENSES="$(command -v go-licenses)"
fi

# Create temporary directory and file
# TEMPFILE is not in TEMPDIR, because this causes infinite recursion later on.
TEMPDIR="$(mktemp -d /tmp/generate-acknowledgements.XXXXXX)"
export TEMPDIR

TEMPFILE="$(mktemp /tmp/acknowledgements.XXXXXX)"
export TEMPFILE

trap 'rm -fr ${TEMPDIR} ${TEMPFILE}' EXIT

"${GO_LICENSES}" save . --save_path="${TEMPDIR}" --force

# Build acknowledgements file
cat > "${TEMPFILE}" <<EOF
# Buildkite Agent OSS Attributions

The Buildkite Agent would not be possible without open-source software.
Licenses for the libraries used are reproduced below.
EOF

addfile() {
    printf "\n\n---\n\n## %s\n\n\`\`\`\n" "${2:-${1#"${TEMPDIR}"/}}" >> "${TEMPFILE}"
    cat "$1" >> "${TEMPFILE}"
    printf "\n\`\`\`\n" >> "${TEMPFILE}"
}

## The Go standard library also counts.
license_path="$(go env GOROOT)/LICENSE"
if [[ ! -f $license_path ]]; then
  # Homebrew and/or macOS does it different? Try up a directory.
  echo "Could not find Go's LICENSE file at $license_path"
  license_path="$(go env GOROOT)/../LICENSE"
fi
if [[ ! -f $license_path ]]; then
  echo "Could not find Go's LICENSE file at $license_path"
  exit 1
fi

addfile "$license_path" "Go standard library"

# Now add all the modules that go-licenses found.
export -f addfile
find "${TEMPDIR}" -type f -print | sort | xargs -I {} bash -c 'addfile "{}"'

# Finally, gzip the file to reduce output binary size, and move into place
gzip -f "${TEMPFILE}"
mv "${TEMPFILE}.gz" clicommand/ACKNOWLEDGEMENTS.md.gz

echo -e "\nGenerated \033[33mclicommand/ACKNOWLEDGEMENTS.md.gz\033[0m 🧑‍💼"

exit 0
