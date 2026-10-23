#!/usr/bin/env bash
set -euo pipefail

dry_run=false
agent_version=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)
      dry_run=true
      shift
      ;;
    --version)
      agent_version="${2:?Missing value for --version}"
      shift 2
      ;;
    *)
      echo "Usage: $0 [--dry-run] [--version X.Y.Z]" >&2
      exit 2
      ;;
  esac
done

if [[ "${DRY_RUN:-}" != "false" && "${dry_run}" != "true" ]]; then
  echo "Skipping Linear sync: no agent release was published in dry-run mode"
  exit 0
fi

if [[ -z "${agent_version}" ]]; then
  agent_version="$(buildkite-agent meta-data get agent-version)"
fi
if [[ ! "${agent_version}" =~ ^4\.[0-9]+\.[0-9]+$ ]]; then
  echo "Error: Linear stable releases require a stable v4 version, got '${agent_version}'" >&2
  exit 1
fi

: "${LINEAR_ACCESS_KEY:?Missing Linear release pipeline access key}"

# The publishing container does not need the checkout agent's Git credentials.
if [[ "$(git rev-parse --is-shallow-repository)" == "true" ]]; then
  git fetch --unshallow https://github.com/buildkite/agent.git
fi
git fetch --tags https://github.com/buildkite/agent.git

# Match the CLI's recent-release window and ancestor check before bootstrapping.
baseline_shas="$(curl --fail --silent --show-error \
  https://api.linear.app/graphql \
  --header "Authorization: ${LINEAR_ACCESS_KEY}" \
  --json '{"query":"query { recentReleasesByAccessKey(limit: 20) { commitSha } }"}' \
  | ruby -rjson -e '
    response = JSON.parse(STDIN.read)
    abort "Linear baseline query failed: #{response.fetch("errors")}" if response.key?("errors")
    response.fetch("data").fetch("recentReleasesByAccessKey").each do |release|
      puts release.fetch("commitSha") if release["commitSha"]
    end
  ')"

has_baseline=false
while IFS= read -r sha; do
  if [[ -n "${sha}" ]] && git cat-file -e "${sha}^{commit}" 2>/dev/null && git merge-base --is-ancestor "${sha}" HEAD; then
    has_baseline=true
    break
  fi
done <<< "${baseline_shas}"

args=(sync --no-branch-ref-detection "--release-version=v${agent_version}")
if [[ "${has_baseline}" == "false" ]]; then
  # Later releases may contain unsynced issues, so keep the bootstrap range fixed.
  integration_commit="$(git log --diff-filter=A --format=%H -- .buildkite/steps/sync-linear-release.sh | tail -n 1)"
  previous_tag="$(git tag --merged "${integration_commit}" --sort=-version:refname \
    | ruby -e '
      version = Gem::Version.new(ARGV.fetch(0))
      puts STDIN.each_line.map(&:strip).find { |tag|
        tag.match?(/\Av4\.\d+\.\d+\z/) && Gem::Version.new(tag.delete_prefix("v")) < version
      }
    ' "${agent_version}")"
  if [[ -z "${previous_tag}" ]]; then
    echo "Error: no previous stable v4 tag is reachable; refusing to sync without a baseline" >&2
    exit 1
  fi
  echo "Bootstrapping Linear release history from ${previous_tag}"
  args+=("--base-ref=${previous_tag}")
fi

if [[ "${dry_run}" == "true" ]]; then
  args+=(--dry-run)
fi

exec linear-release "${args[@]}"
