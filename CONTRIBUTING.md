# Contributing to the Agent

1. Fork this repo
1. Create a feature branch with a nice name (`git checkout -b my-new-feature`)
1. Write your code!
    - Make sure your code is correctly formatted by running `go tool gofumpt -extra -w .`, and that the tests are passing by running `go test ./...`
1. Commit your changes (`git commit -am 'Add some feature'`)
    - In an ideal world we have [atomic commits](https://www.pauline-vos.nl/atomic-commits/) and use [Tim Pope-style commit messages](https://tbaggery.com/2008/04/19/a-note-about-git-commit-messages.html), but so long as it's clear what's happening in your PR, that's fine. We try to not be super persnickety about these things.
1. Push to your branch (`git push origin my-new-feature`)
1. Create a pull request for your branch. Make sure that your PR has a nice description (fill in the template), and that it's linked to any relevant issues.

The agent wranglers at Buildkite will review your PR, and start a CI build for it. For security reasons, we don't automatically run CI against forked repos, and a human will review your PR prior to its CI running.

Our objective is to have no PR wait more than a week for some sort of interaction from us -- this might be a review, or it might be a "I'm going to come back to this and review it a bit later". This isn't a guarantee though, and sometimes other work might get in the way of reviewing opensource contributions. If we're really dragging our feet on reviewing a PR, please feel free to ping us through GitHub or Slack, or get in touch with support@buildkite.com, and they can bug us to get things done :)

## Nix releases

After GitHub publishes a stable agent release, the Buildkite **Propose Nix
release bump** step opens a manifest-only PR from `release/nix/v<version>`.
Review and merge it after **Nix release gate** passes. That check covers flake
evaluation and native Linux/macOS amd64/arm64 builds and version checks. Do
not enable auto-merge. Update nixpkgs in a separate PR with
`nix flake update nixpkgs`.

If the bump step soft-fails, check its log and retry that step, not the GitHub
release. It reuses the branch and PR if they already exist. It does not reopen
a deliberately closed PR or replace an equal or newer version on `main`.

Repository setup: require **Nix release gate** from GitHub Actions on `main`,
in addition to the existing checks and human review. Require branches to be
up to date before merging so the gate compares against the current manifest.
The stable release credential must be able to push the bump branch, open and
label a PR, and trigger its workflows. Use a GitHub App token or PAT, not an
Actions `GITHUB_TOKEN`.
