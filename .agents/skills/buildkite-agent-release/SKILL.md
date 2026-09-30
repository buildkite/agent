---
name: buildkite-agent-release
description: Prepare for a Buildkite Agent Release (v4 from main, or v3 from the v3 branch).
---

# Buildkite Agent Release

## When to Use This Skill

Use this skill when you need to:
* Prepare a release for Buildkite Agent.

## Instructions

### 1. Ask user which release line this is

The repo has two active release lines:

* **v4**: released from `main` (e.g. `4.1.0`).
* **v3**: released from the `v3` branch (e.g. `3.138.0`).

Ask the user to pick v4 or v3 before doing anything else. The chosen line determines the base branch for everything below: `main` for v4, `v3` for v3.

### 2. Double check the current commit is up to date with the release branch

The current commit should be up to date with the latest `origin/main` (v4) or `origin/v3` (v3).

### 3. Find the previous release and preview what's changed

Find the latest release on the chosen line. `gh release view` on its own returns the repo-wide latest release, which is the wrong line for v3, so list them instead:

```bash
gh release list --repo buildkite/agent --limit 20
```

Generate the release notes GitHub will publish, using a placeholder tag name because the version hasn't been decided yet. Pass `previous_tag_name` explicitly so the preview is guaranteed to diff against the right release on the chosen line. Set `target_commitish` to the release branch:

```bash
mkdir -p tmp
gh api -X POST repos/buildkite/agent/releases/generate-notes \
  -f tag_name=v4.0.8-next \
  -f target_commitish=main \
  -f previous_tag_name=v4.0.8 \
  --jq .body > tmp/release-notes.md
```

This is a read-only API call; it doesn't create a release or tag. Categories come from the PR labels via [.github/release.yml](../../../.github/release.yml). The `tmp/` directory is gitignored.

Read every entry in `tmp/release-notes.md`, including 🏠 Internal. Any unlabelled PR lands in Internal, and so do mislabelled user-facing ones. For anything whose impact isn't obvious from its title, read the PR (`gh pr view <number> --repo buildkite/agent`). If a PR is mis-categorised, fix its labels and re-run the command.

### 4. Decide the version number with the user

Classify each change using SemVer, applied to what users of the agent can observe:

* **Major**: breaks existing usage, e.g. removes or renames a flag, command, env var or config option, or drops platform support. Stop and discuss with the user; this shouldn't happen on an established line without a plan.
* **Minor**: adds or changes something users can reach or observe. This includes:
  * new commands or subcommands (including hidden ones), flags, env vars, config options, experiments, Job API routes, or pipeline/API fields the agent now understands
  * promoting an experiment, or enabling a capability by default
  * changing a default or existing behaviour, even if a flag restores the old behaviour
  * new user-visible output, such as new log reports or diagnostics
* **Patch**: bug fixes, security fixes, dependency bumps, docs, help-text and wording fixes, and internal or CI-only changes. A fix that makes the agent behave as it was already documented or intended to is still a patch.

The highest classification of any change wins. One ✨ Added entry makes the whole release a minor. A change being experimental or hidden doesn't make it a patch; it's still new surface area, it's just a "patchier" minor. When a change sits on the boundary, round up to minor. Minor version numbers are cheap, and under-classifying hides new behaviour from people reading version numbers.

Don't just ask "minor or patch?". Present a recommendation and your reasoning, then have the user confirm or override it:

* the recommended version, e.g. `4.0.8` → `4.1.0`
* the changes that drive the classification, with PR links, and why they count as minor (or why nothing does)
* any borderline changes, and which way you leaned

Prompt the user to sanity-check with SemVer in mind: "Is there anything here someone could start depending on, or would notice has changed, after upgrading? If so, it's at least a minor." Wait for their answer before continuing.

Only cut a pre-release (e.g. `4.1.0-beta.1`) if the user explicitly asks for one.

### 5. Update the agent version file

Edit `version/VERSION` to the new version number. Use the bare semver (e.g. `4.1.0`), not a `v`-prefixed tag.

### 6. Regenerate the release notes with the real tag

Re-run the command from step 3 with the chosen `tag_name` (e.g. `v4.1.0`) so the "Full Changelog" link is correct.

### 7. Suggest an editorial pass on the changelog

The generated notes are built from PR titles, which are written for reviewers rather than agent users. Prompt the user to do an editorial pass, and make it easy by suggesting rewritten titles for any entry in a user-facing category (🔒 Security, ✨ Added, 🐛 Fixed, 🔧 Changed) that would read poorly to someone outside Buildkite. Good titles:

* drop internal ticket IDs (`A-1234:`, `[SUP-5678]`, `PS-2073:`), since the PR link leads back to the ticket anyway
* drop conventional-commit prefixes (`feat:`, `fix(cache):`) and `[Backport]` tags; the category heading already says what kind of change it is
* describe the effect on users, not the implementation, e.g. "Stop leaking job log temp files and file descriptors when `enable-job-log-tmpfile` is set" rather than "Close the job log tmpfile once the job is done, not when the process exits"
* name the command, flag or setting involved, e.g. `buildkite-agent cache save --force`
* start with a capitalised verb and don't end with a full stop

Present the suggestions as a complete, copy-pasteable edited version of the release notes. Keep the generated format: the same headings, one bullet per PR, and the `by @author in <PR URL>` attribution. Write it to `tmp/release-notes-edited.md` and show it in a fenced `markdown` code block, so it's easy to copy.

Ask the user to review it and tell you what to change. It's fine for them to drop unimportant entries.

### 8. Create the release PR

* Create a new branch for the release (e.g. `release/v4.1.0`), based on the release branch.
* Commit the `version/VERSION` change.
* Push the branch and open a PR using `gh pr create` against the release branch (`--base main` for v4, `--base v3` for v3):
    * Title: `release: v4.1.0` (matching the convention from previous release PRs).
    * Body: contents of `tmp/release-notes-edited.md` from the previous step, or `tmp/release-notes.md` if the user skipped the editorial pass.
    * Label: `release`. This is required so the PR-labels workflow passes, and so the release PR itself is excluded from its own auto-generated notes (configured in [.github/release.yml](../../../.github/release.yml)).
    * Example: `gh pr create --base main --title "release: v4.1.0" --body-file tmp/release-notes-edited.md --label release`.

The PR body is for human review only. The actual release notes are regenerated from PR titles and labels by `gh release create --generate-notes` in [.buildkite/steps/github-release.sh](../../../.buildkite/steps/github-release.sh) when the release pipeline runs. If the user did an editorial pass, remind them to replace the notes on the GitHub release once it's published: `gh release edit v4.1.0 --repo buildkite/agent --notes-file tmp/release-notes-edited.md`.

### 9. Done

* The user may want to review the published notes on GitHub.
