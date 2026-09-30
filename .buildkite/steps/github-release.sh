#!/usr/bin/env bash
set -e

dry_run() {
  if [[ "${DRY_RUN:-}" == "false" ]] ; then
    "$@"
  else
    echo "[dry-run] $*"
  fi
}

echo '--- Getting credentials from SSM'
GH_TOKEN="$(aws ssm get-parameter \
  --name /pipelines/agent/GITHUB_RELEASE_ACCESS_TOKEN \
  --with-decryption \
  --output text \
  --query Parameter.Value \
  --region us-east-1 \
)"
export GH_TOKEN

if [[ "${GH_TOKEN}" == "" ]]; then
  echo "Error: Missing \$GH_TOKEN"
  exit 1
fi

echo '--- Installing gh CLI'
GH_VERSION=2.96.0
curl -fsSL "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_amd64.tar.gz" \
  | tar -xz -C /tmp
install "/tmp/gh_${GH_VERSION}_linux_amd64/bin/gh" /usr/local/bin/gh
gh --version

echo '--- Getting agent version from build meta data'

export FULL_AGENT_VERSION="$(buildkite-agent meta-data get "agent-version-full")"
export AGENT_VERSION="$(buildkite-agent meta-data get "agent-version")"
export BUILD_VERSION="$(buildkite-agent meta-data get "agent-version-build")"
export IS_PRERELEASE="$(buildkite-agent meta-data get "agent-is-prerelease")"

echo "Full agent version: ${FULL_AGENT_VERSION}"
echo "Agent version: ${AGENT_VERSION}"
echo "Build version: ${BUILD_VERSION}"
echo "Is prerelease?: ${IS_PRERELEASE}"

if [[ "${CODENAME}" == "unstable" && "${IS_PRERELEASE}" == "0" ]] ; then
  echo "Skipping github release, will happen in stable/oldstable pipeline"
  exit 0
fi

if [[ ("${CODENAME}" == "stable" || "${CODENAME}" == "oldstable") && "${IS_PRERELEASE}" == "1" ]] ; then
  echo "Skipping github release, should have happened in unstable pipeline"
  exit 0
fi

echo '--- Downloading releases'

artifacts_build="$(buildkite-agent meta-data get "agent-artifacts-build")"

rm -rf releases
mkdir -p releases
buildkite-agent artifact download --build "${artifacts_build}" "releases/*" .

echo "Version is ${FULL_AGENT_VERSION}"

release_args=(
  "v${AGENT_VERSION}"
  releases/*
  --repo buildkite/agent
  --target "$(git rev-parse HEAD)"
)

echo '--- Finding release notes'

# The release PR's body holds the editorially reviewed release notes. Use it if
# this commit is the merge of a release PR, otherwise fall back to GitHub's
# generated notes. A failed lookup fails the step (via set -e) rather than
# silently publishing generated notes in place of the reviewed ones.
release_pr_body="$(gh api "repos/buildkite/agent/commits/$(git rev-parse HEAD)/pulls" \
  --jq "[.[] | select(.merge_commit_sha == \"$(git rev-parse HEAD)\" and any(.labels[]; .name == \"release\"))][0].body // empty")"
release_pr_body="${release_pr_body//$'\r'/}"

if [[ -n "${release_pr_body//[[:space:]]/}" ]]; then
  echo "Using the release PR body as the release notes"
  printf '%s\n' "${release_pr_body}" > release-notes.md
  cat release-notes.md
  release_args+=(--title "v${AGENT_VERSION}" --notes-file release-notes.md)
else
  echo "No release PR body found, using GitHub's generated notes"
  release_args+=(--generate-notes)
fi

if [[ "${IS_PRERELEASE}" == "1" ]]; then
  echo "--- 🚀 ${AGENT_VERSION} (prerelease)"

  buildkite-agent meta-data set github_release_type "prerelease"

  release_args+=(--prerelease)

elif [[ "${CODENAME}" == "oldstable" ]]; then
  echo "--- 🚀 ${AGENT_VERSION} (oldstable)"

  buildkite-agent meta-data set github_release_type "stable"

  release_args+=(--latest=false)

else
  echo "--- 🚀 ${AGENT_VERSION}"

  buildkite-agent meta-data set github_release_type "stable"
fi

buildkite-agent meta-data set github_release_version "${AGENT_VERSION}"

dry_run gh release create "${release_args[@]}"
