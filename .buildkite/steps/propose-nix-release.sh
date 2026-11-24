#!/usr/bin/env bash
set -euo pipefail

if [[ "${DRY_RUN:-}" != "false" ]]; then
  echo 'Skipping Nix bump: no agent release was published'
  exit 0
fi

version=$(buildkite-agent meta-data get agent-version)
prerelease=$(buildkite-agent meta-data get agent-is-prerelease)
if [[ "$prerelease" == 1 ]]; then
  echo "Skipping Nix bump: ${version} is a prerelease"
  exit 0
fi
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Invalid stable agent version: $version" >&2
  exit 1
fi

repo=buildkite/agent
base=main
tag="v${version}"
branch="release/nix/${tag}"
manifest=nix/release.json
generator="$(git rev-parse --show-toplevel)/scripts/nix-release-manifest.rb"

export GH_TOKEN
GH_TOKEN=$(aws ssm get-parameter \
  --name /pipelines/agent/GITHUB_RELEASE_ACCESS_TOKEN \
  --with-decryption --output text --query Parameter.Value --region us-east-1)
: "${GH_TOKEN:?Missing GitHub release credential}"

# Start from main, not the release checkout (which may be from an old build).
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
git clone --quiet --depth 1 --branch "$base" "https://github.com/${repo}.git" "$scratch/repo"
cd "$scratch/repo"
current=$(ruby -rjson -e 'puts JSON.parse(File.read(ARGV[0])).fetch("version")' "$manifest")
if [[ "$version" == "$current" || "$(printf '%s\n' "$current" "$version" | sort -V | tail -1)" != "$version" ]]; then
  echo "Skipping Nix bump: main already pins $current (release is $version)"
  exit 0
fi

# A closed PR is a decision, not a reason to create another one on retry.
existing=$(gh api --method GET "repos/$repo/pulls" -f state=all -f base="$base" \
  -f head="${repo%%/*}:$branch" \
  --jq '.[0] | select(. != null) | [.html_url, .state, any(.labels[]; .name == "internal")] | @tsv')
if [[ -n "$existing" ]]; then
  IFS=$'\t' read -r url pr_state labeled <<< "$existing"
  echo "Nix bump already proposed: $url"
  if [[ "$pr_state" == open && "$labeled" == false ]]; then
    gh pr edit "$url" --add-label internal
  fi
  exit 0
fi

published=$(gh release view "$tag" --repo "$repo" --json isDraft,isPrerelease --jq '(.isDraft or .isPrerelease) | not')
if [[ "$published" != true ]]; then
  echo "Expected a published stable GitHub release: $tag" >&2
  exit 1
fi
gh release download "$tag" --repo "$repo" --pattern "buildkite-agent-${version}.SHA256SUMS" --output - \
  | ruby "$generator" "$version" > "$scratch/release.json"

existing_branch=$(git ls-remote --heads origin "refs/heads/$branch")
if [[ -n "$existing_branch" ]]; then
  # A previous run may have pushed the commit but failed to open the PR.
  git fetch --quiet --depth 2 origin "refs/heads/$branch"
  git show "FETCH_HEAD:$manifest" | cmp - "$scratch/release.json"
  if [[ "$(git diff --name-only FETCH_HEAD^ FETCH_HEAD)" != "$manifest" ]]; then
    echo "Existing bump branch does not contain a manifest-only commit: $branch" >&2
    exit 1
  fi
else
  cp "$scratch/release.json" "$manifest"
  git add -- "$manifest"
  git -c user.name='Buildkite Release' -c user.email='support@buildkite.com' \
    commit -m "Update Nix package to Buildkite Agent $tag"
  # Create only. Never overwrite an existing branch or a maintainer's edits.
  git -c credential.helper= -c 'credential.helper=!gh auth git-credential' \
    push --force-with-lease="refs/heads/$branch:" origin "HEAD:refs/heads/$branch"
fi

gh pr create --repo "$repo" --base "$base" --head "$branch" --label internal \
  --title "Update Nix package to Buildkite Agent $tag" \
  --body "🤖

Pin the Nix package to [Buildkite Agent $tag](https://github.com/${repo}/releases/tag/${tag}) using the published SHA-256 checksums. The nixpkgs lock is unchanged.

The Nix workflow evaluates all four outputs and checks the installed binary's version on each native runner. Review and merge manually once the checks pass."
