## [v3.74.0](https://github.com/buildkite/agent/tree/v3.74.0) (2026-04-17)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.73.1...v3.74.0)

### Security
- ⚠️ When using `artifact download`, artifacts that were uploaded with paths containing `..` will no longer be able to traverse up from the destination path. This change is unlikely to break the vast majority of pipelines, however if you are relying on `..` for path traversal and cannot fix your pipeline, you can enable the new experiment `allow-artifact-path-traversal` [#2815](https://github.com/buildkite/agent/pull/2815) (@DrJosh9000)
- Redact Job API token like other env vars [#2834](https://github.com/buildkite/agent/pull/2834) (@DrJosh9000)

### Added
- Add logs to allowed-[repositories|plugins] [#2810](https://github.com/buildkite/agent/pull/2810) (@jakubm-canva)

### Fixed
- Fix error in k8s after job completes [#2804](https://github.com/buildkite/agent/pull/2804) (@DrJosh9000)

### Changed
- PTY rows/cols increased [#2806](https://github.com/buildkite/agent/pull/2806) (@pda)
- Dont sign initial steps with interpolations [#2813](https://github.com/buildkite/agent/pull/2813) (@moskyb)

### Internal
- kubernetes-exec is now a flag [#2814](https://github.com/buildkite/agent/pull/2814) (@DrJosh9000)
- shell logger: Use fmt functions once [#2805](https://github.com/buildkite/agent/pull/2805) (@DrJosh9000)
- Update deprecated import [#2811](https://github.com/buildkite/agent/pull/2811) (@DrJosh9000)
- Use Rand per-test in agent/plugin/error_test.go [#2795](https://github.com/buildkite/agent/pull/2795) (@moskyb)
- Publish debian and rpm packages to Buildkite Packages [#2824](https://github.com/buildkite/agent/pull/2824) [#2826](https://github.com/buildkite/agent/pull/2826) [#2831](https://github.com/buildkite/agent/pull/2831) [#2830](https://github.com/buildkite/agent/pull/2830) [#2833](https://github.com/buildkite/agent/pull/2833) (@sj26)
- Dependabot updates: [#2809](https://github.com/buildkite/agent/pull/2809), [#2816](https://github.com/buildkite/agent/pull/2816), [#2800](https://github.com/buildkite/agent/pull/2800), [#2801](https://github.com/buildkite/agent/pull/2801), [#2802](https://github.com/buildkite/agent/pull/2802), [#2803](https://github.com/buildkite/agent/pull/2803), [#2787](https://github.com/buildkite/agent/pull/2787), [#2798](https://github.com/buildkite/agent/pull/2798), [#2808](https://github.com/buildkite/agent/pull/2808), [#2827](https://github.com/buildkite/agent/pull/2827) [#2817](https://github.com/buildkite/agent/pull/2817), [#2818](https://github.com/buildkite/agent/pull/2818), [#2819](https://github.com/buildkite/agent/pull/2819), [#2822](https://github.com/buildkite/agent/pull/2822), [#2829](https://github.com/buildkite/agent/pull/2829), [#2832](https://github.com/buildkite/agent/pull/2832), [#2835](https://github.com/buildkite/agent/pull/2835) (@dependabot[bot])
