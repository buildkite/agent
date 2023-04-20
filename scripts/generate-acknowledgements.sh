#!/usr/bin/env bash
set -euo pipefail

if [[ ! -f "./go.mod" ]]; then
    echo "Need to run this from the directory containing go.mod"
    exit 1
fi

OUTPUT_FILE="ACKNOWLEDGEMENTS.md"

# Ensure modules are downloaded
go mod download

# Get go-licenses tool
if ! command -v go-licenses >/dev/null; then
	go install github.com/google/go-licenses@latest
	GO_LICENSES="$(go env GOPATH)/bin/go-licenses"
else
	GO_LICENSES="$(command -v go-licenses)"
fi

# Save licenses
export TEMPDIR="$(mktemp -d /tmp/generate-acknowledgements.XXXXXX)" || exit 1
export COMBINEDIR="${TEMPDIR}/combined"
mkdir -p "${COMBINEDIR}"

# go-licenses output can vary by GOOS.
# https://github.com/google/go-licenses/issues/187
# Run it for each OS we release for, and combine the results.
for goos in darwin dragonfly freebsd linux netbsd openbsd windows ; do
  GOOS="${goos}" "${GO_LICENSES}" save . --save_path="${TEMPDIR}/${goos}"
  cp -fR "${TEMPDIR}/${goos}"/* "${COMBINEDIR}"
done

trap "rm -fr ${TEMPDIR}" EXIT

# Build acknowledgements file
export TEMPFILE="$(mktemp acknowledgements.XXXXXX)" || exit 1
trap "rm -f ${TEMPFILE}" EXIT
cat > "${TEMPFILE}" <<EOF
# Buildkite Agent OSS Attributions

The Buildkite Agent would not be possible without open-source software.
Licenses for the libraries used are reproduced below.
EOF

addfile() {
    printf "\n\n---\n\n## %s\n\n\`\`\`\n" "${2:-${1#${COMBINEDIR}/}}" >> "${TEMPFILE}"
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

## Now add all the modules.
export -f addfile
find "${COMBINEDIR}" -type f -print | sort | xargs -I {} bash -c 'addfile "{}"'

## Add trailer
printf "\n\n---\n\nFile generated using %s\n%s\n" "$0" "$(date)" >> "${TEMPFILE}"

# Replace existing acknowledgements file
mv "${TEMPFILE}" "${OUTPUT_FILE}"
chmod 644 "${OUTPUT_FILE}"

# gzipped version for embedding purposes
gzip -kf "${OUTPUT_FILE}"
mv "${OUTPUT_FILE}.gz" clicommand/

exit 0
