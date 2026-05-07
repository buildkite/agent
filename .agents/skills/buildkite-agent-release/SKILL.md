---
name: buildkite-agent-release
description: Prepare for a Buildkite Agent Release.
---

# Buildkite Agent Release

## When to Use This Skill

Use this skill when you need to:
* Prepare a release for Buildkite Agent.

## Instructions

### 1. Double check the current commit is up to date with latest in main branch.

### 2. Ask user to decide if this is a minor version bump or patch version bump.

Find the latest Buildkite Agent version using `gh`: `gh release view --repo buildkite/agent --json tagName,publishedAt`

Ask user to decide whether it's a minor version bump or patch version bump.

### 3. Generate a list of changes

Use [ghch](https://github.com/buildkite/ghch) to generate our changelogs, and
then human brains to edit them down into something other humans will
want to read.

To preview the changes run:

```
ghch --format=markdown --from=v3.xx.yy --next-version=v3.xx.yy+1
```

This will print a list of all the changes that are ready to go out. Looking at
the list. You can re-run
the `ghch` command with a different version number if you decide to change it
before releasing.


Edit [CHANGELOG.md](https://github.com/buildkite/agent/blob/main/CHANGELOG.md) file with paste in the list from the `ghch` output. This will likely need some cleaning up and editing as it only lists the names of the PRs.

Try to make each line short but descriptive - we want people to be able to understand the general gist of the change without having to read paragraphs or go into the PR itself.

The changelog should be split up into sections:

- Security
    - e.g. “Updated to new version of go-yellow to fix YellowToad CVE”
- Changed
    - e.g. “Logs are now all printed in yellow to be easier on the eyes”
    - If the `go.mod` Go version or toolchan version changed, and it’s not already
    listed under Security, be sure to list it here!
- Added
    - e.g. “Added the buildkite-agent ‘yellow’ subcommand”,
- Fixed
    - e.g. “Fixed bug causing all logs to be printed in yellow”
- Internal
    - e.g. “Reformatted the pipeline.yml”

Use your best judgement when it comes to putting things in the right section,
and if a section doesn’t have any PRs in it, get rid of the heading.

Conventionally, we lump all the Dependabot updates into a single line in the
Internal section, since they tend to be invisibile to customers. But if a
dependency was updated that fixes a big security issue or changes some important
behaviour (for example), then it should be called out separately!

Also ensure the date is the date the release is being made.

As an example, see the example_changelog for v3.74.0 in the skill folder.

### 4. Update the agent version file

Edit the `version/VERSION` to update the value to the new version number. Use the bare semver (e.g. `3.75.0`), not a `v`-prefixed tag (e.g. not `v3.75.0`).

### 5. Create the release PR

* Create a new branch for the release (e.g. `release/v3.75.0`).
* Commit the `CHANGELOG.md` and `version/VERSION` changes.
* Push the branch and open a PR using `gh pr create` against `main`:
    * Title: `release: v3.75.0` (matching the convention from previous release PRs).
    * Body: paste the same changelog section that was added to `CHANGELOG.md` for this release (header line, Full Changelog link, and all sections). See [PR #3823](https://github.com/buildkite/agent/pull/3823) for an example.

### 6. Done

* Remind user to unblock the release.
* Remind user to update Github Release.
