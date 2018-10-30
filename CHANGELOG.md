# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic Versioning](http://semver.org/spec/v2.0.0.html).

## [v3.5.4](https://github.com/buildkite/agent/tree/v3.5.4) (2018-10-24)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.5.3...v3.5.4)

### Fixed
- Prevent docker image from crashing with missing config error [#847](https://github.com/buildkite/agent/pull/847) (@lox)

## [v3.5.3](https://github.com/buildkite/agent/tree/v3.5.3) (2018-10-24)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.5.2...v3.5.3)

### Fixed
- Update to alpine to 3.8 in docker image [#842](https://github.com/buildkite/agent/pull/842) (@lox)
- Set BUILDKITE_AGENT_CONFIG in docker images to /buildkite [#834](https://github.com/buildkite/agent/pull/834) (@blakestoddard)
- Fix agent panics on ARM architecture [#831](https://github.com/buildkite/agent/pull/831) (@jhedev)

## [v3.5.2](https://github.com/buildkite/agent/tree/v3.5.2) (2018-10-09)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.5.1...v3.5.2)

### Changed
- Fix issue where pipelines with a top-level array of steps failed [#830](https://github.com/buildkite/agent/pull/830) (@lox)

## [v3.5.1](https://github.com/buildkite/agent/tree/v3.5.1) (2018-10-08)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.5.0...v3.5.1)

### Fixed
- Ensure plugin directory exists, otherwise checkout lock thrashes [#828](https://github.com/buildkite/agent/pull/828) (@lox)

## [v3.5.0](https://github.com/buildkite/agent/tree/v3.5.0) (2018-10-08)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.4.0...v3.5.0)

### Fixed
- Add plugin locking before checkout [#827](https://github.com/buildkite/agent/pull/827) (@lox)
- Ensure pipeline parser maintains map order in output [#824](https://github.com/buildkite/agent/pull/824) (@lox)
- Update aws sdk [#818](https://github.com/buildkite/agent/pull/818) (@sj26)
- Fix boostrap typo [#814](https://github.com/buildkite/agent/pull/814) (@ChefAustin)

### Changed
- `annotate` takes body as an arg, or reads from a pipe [#813](https://github.com/buildkite/agent/pull/813) (@sj26)
- Respect pre-set BUILDKITE_BUILD_CHECKOUT_PATH [#806](https://github.com/buildkite/agent/pull/806) (@lox)
- Add time since last successful heartbeat/ping [#810](https://github.com/buildkite/agent/pull/810) (@lox)
- Updating launchd templates to only restart on error [#804](https://github.com/buildkite/agent/pull/804) (@lox)
- Allow more time for systemd graceful stop [#819](https://github.com/buildkite/agent/pull/819) (@lox)

## [v3.4.0](https://github.com/buildkite/agent/tree/v3.4.0) (2018-07-18)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.3.0...v3.4.0)

### Changed
- Add basic plugin definition parsing [#748](https://github.com/buildkite/agent/pull/748) (@lox)
- Allow specifying which phases bootstrap should execute [#799](https://github.com/buildkite/agent/pull/799) (@lox)
- Warn in bootstrap when protected env are used [#796](https://github.com/buildkite/agent/pull/796) (@lox)
- Cancellation on windows kills bootstrap subprocesses [#795](https://github.com/buildkite/agent/pull/795) (@amitsaha)

## [v3.3.0](https://github.com/buildkite/agent/tree/v3.3.0) (2018-07-11)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.2.1...v3.3.0)

### Added
- Allow tags from the host to be automatically added with --add-host-tags [#791](https://github.com/buildkite/agent/pull/791) (@lox)
- Allow --no-plugins=false to force plugins on [#790](https://github.com/buildkite/agent/pull/790) (@lox)

## [v3.2.1](https://github.com/buildkite/agent/tree/v3.2.1) (2018-06-28)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.2.0...v3.2.1)

### Changed
- Remove the checkout dir when git clean fails [#786](https://github.com/buildkite/agent/pull/786) (@lox)
- Add a --dry-run to pipeline upload that dumps json [#781](https://github.com/buildkite/agent/pull/781) (@lox)
- Support PTY under OpenBSD [#785](https://github.com/buildkite/agent/pull/785) (@derekmarcotte) [#787](https://github.com/buildkite/agent/pull/787) (@derekmarcotte)
- Experiments docs and experiment cleanup [#771](https://github.com/buildkite/agent/pull/771) (@lox)

## [v3.2.0](https://github.com/buildkite/agent/tree/v3.2.0) (2018-05-25)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.1.2...v3.2.0)

### Changed
- Propagate exit code > 1 out of failing hooks [#768](https://github.com/buildkite/agent/pull/768) (@lox)
- Fix broken list parsing in cli arguments --tags and --experiments [#772](https://github.com/buildkite/agent/pull/772) (@lox)
- Add a virtual provides to the RPM package [#737](https://github.com/buildkite/agent/pull/737) (@jnewbigin)
- Clean up docker image building [#755](https://github.com/buildkite/agent/pull/755) (@lox)
- Don't trim whitespace from the annotation body [#766](https://github.com/buildkite/agent/pull/766) (@petemounce)

## [v3.1.2](https://github.com/buildkite/agent/tree/v3.1.2) (2018-05-10)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.1.1...v3.1.2)

### Changed
- Experiment: Pass jobs an authenticated unix socket rather than an access token  [#759](https://github.com/buildkite/agent/pull/759) (@lox)
- Remove buildkite:git:branch meta-data [#753](https://github.com/buildkite/agent/pull/753) (@sj26)
- Set TERM and PWD for commands that get executed in shell [#751](https://github.com/buildkite/agent/pull/751) (@lox)

### Fixed
- Avoid pausing after job has finished [#764](https://github.com/buildkite/agent/pull/764) (@sj26)

## [v3.1.1](https://github.com/buildkite/agent/tree/v3.1.1) (2018-05-02)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.1.0...v3.1.1)

### Fixed
- Fix stdin detection for output redirection [#750](https://github.com/buildkite/agent/pull/750) (@lox)

## [v3.1.0](https://github.com/buildkite/agent/tree/v3.1.0) (2018-05-01)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0.1...v3.1.0)

### Changed
- Add ubuntu docker image [#749](https://github.com/buildkite/agent/pull/749) (@lox)
- Support `--no-interpolation` option in `pipeline upload` [#733](https://github.com/buildkite/agent/pull/733) (@lox)
- Bump our Docker image base to alpine v3.7 [#745](https://github.com/buildkite/agent/pull/745) (@sj26)
- Better error for multiple file args to artifact upload [#740](https://github.com/buildkite/agent/pull/740) (@toolmantim)

## [v3.0.1](https://github.com/buildkite/agent/tree/v3.0.1) (2018-04-17)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0.0...v3.0.1)

### Changed
- Don't set Content-Encoding on s3 upload [#732] (@lox)

## [v3.0.0](https://github.com/buildkite/agent/tree/v3.0.0) (2018-04-03)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.44...v3.0.0)

No changes

## [v3.0-beta.44](https://github.com/buildkite/agent/tree/v3.0-beta.44) (2018-04-03)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.43...v3.0-beta.44)

### Fixed
- Normalize the `bootstrap-script` command using a new `commandpath` normalization [#714](https://github.com/buildkite/agent/pull/714) (@keithpitt)

### Changed
- Install windows binary to c:\buildkite-agent\bin [#713](https://github.com/buildkite/agent/pull/713) (@lox)

## [v3.0-beta.43](https://github.com/buildkite/agent/tree/v3.0-beta.43) (2018-04-03)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.42...v3.0-beta.43)

### Changed
- Prettier bootstrap output 💅🏻 [#708](https://github.com/buildkite/agent/pull/708) (@lox)
- Only run git submodule operations if there is a .gitmodules [#704](https://github.com/buildkite/agent/pull/704) (@lox)
- Add an agent config for no-local-hooks [#707](https://github.com/buildkite/agent/pull/707) (@lox)
- Build docker image as part of agent pipeline [#701](https://github.com/buildkite/agent/pull/701) (@lox)
- Windows install script [#699](https://github.com/buildkite/agent/pull/699) (@lox)
- Expose no-git-submodules config and arg to start [#698](https://github.com/buildkite/agent/pull/698) (@lox)

## [v3.0-beta.42](https://github.com/buildkite/agent/tree/v3.0-beta.42) (2018-03-20)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.41...v3.0-beta.42)

### Fixed
- Preserve types in pipeline.yml [#696](https://github.com/buildkite/agent/pull/696) (@lox)

## [v3.0-beta.41](https://github.com/buildkite/agent/tree/v3.0-beta.41) (2018-03-16)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.40...v3.0-beta.41)

### Added
- Retry failed checkouts [#670](https://github.com/buildkite/agent/pull/670) (@lox)

### Changed
- Write temporary batch scripts for Windows/CMD.EXE [#692](https://github.com/buildkite/agent/pull/692) (@lox)
- Enabling `no-command-eval` will also disable use of plugins [#690](https://github.com/buildkite/agent/pull/690) (@keithpitt)
- Support plugins that have a `null` config [#691](https://github.com/buildkite/agent/pull/691) (@keithpitt)
- Handle upgrading bootstrap-path from old 2.x shell script [#580](https://github.com/buildkite/agent/pull/580) (@lox)
- Show plugin commit if it's already installed [#685](https://github.com/buildkite/agent/pull/685) (@keithpitt)
- Handle windows paths in all usage of shellwords parsing [#686](https://github.com/buildkite/agent/pull/686) (@lox)
- Make NormalizeFilePath handle empty strings and windows [#688](https://github.com/buildkite/agent/pull/688) (@lox)
- Retry ssh-keyscans on error or blank output [#687](https://github.com/buildkite/agent/pull/687) (@keithpitt)
- Quote and escape env-file values [#682](https://github.com/buildkite/agent/pull/682) (@lox)
- Prevent incorrect corrupt git checkout detection on fresh checkout dir creation [#681](https://github.com/buildkite/agent/pull/681) (@lox)
- Only keyscan git/ssh urls [#675](https://github.com/buildkite/agent/pull/675) (@lox)
- Fail the job when no command is provided in the default command phase [#678](https://github.com/buildkite/agent/pull/678) (@keithpitt)
- Don't look for powershell hooks since we don't support them yet [#679](https://github.com/buildkite/agent/pull/679) (@keithpitt)
- Exit when artifacts can't be found for downloading [#676](https://github.com/buildkite/agent/pull/676) (@keithpitt)
- Run scripts via the shell, rather than invoking with exec [#673](https://github.com/buildkite/agent/pull/673) (@lox)
- Rename no-automatic-ssh-fingerprint-verification to no-ssh-keyscan [#671](https://github.com/buildkite/agent/pull/671) (@lox)

### Fixed
- Parse pipeline.yml env block in order [#668](https://github.com/buildkite/agent/pull/668) (@lox)
- Bootstrap shouldn't panic if plugin checkout fails [#672](https://github.com/buildkite/agent/pull/672) (@lox)

## [v3.0-beta.40](https://github.com/buildkite/agent/tree/v3.0-beta.40) (2018-03-07)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.39...v3.0-beta.40)

### Changed

- Commands are no longer written to temporary script files before execution [#648](https://github.com/buildkite/agent/pull/648) (@lox)
- Support more complex types in plugin config [#658](https://github.com/buildkite/agent/pull/658) (@lox)

### Added

- Write an env-file for the bootstrap [#643](https://github.com/buildkite/agent/pull/643) (@DazWorrall)
- Allow the shell interpreter to be configured [#648](https://github.com/buildkite/agent/pull/648) (@lox)

### Fixed

- Fix stdin detection on windows [#665](https://github.com/buildkite/agent/pull/665) (@lox)
- Check hook scripts get written to disk without error [#652](https://github.com/buildkite/agent/pull/652) (@sj26)

## [v3.0-beta.39](https://github.com/buildkite/agent/tree/v3.0-beta.39) (2018-01-31)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.38...v3.0-beta.39)

### Fixed

- Fix bug failing artifact upload glob would cause later globs to fail [\#620](https://github.com/buildkite/agent/pull/620) (@lox)
- Fix race condition in process management [\#618](https://github.com/buildkite/agent/pull/618) (@lox)
- Support older git versions for submodule commands [\#628](https://github.com/buildkite/agent/pull/628) (@lox)
- Lots of windows fixes and tests! [\#630](https://github.com/buildkite/agent/pull/630) [\#631](https://github.com/buildkite/agent/pull/631) [\#632](https://github.com/buildkite/agent/pull/632)

### Added

- Support for Bash for Windows for plugins and hooks! [\#636](https://github.com/buildkite/agent/pull/636) (@lox)
- Correct mimetypes for .log files [\#635](https://github.com/buildkite/agent/pull/635) (@DazWorrall)
- Usable Content-Disposition for GCE uploaded artifacts [\#640](https://github.com/buildkite/agent/pull/640) (@DazWorrall)
- Experiment for retrying checkout on failure [\#613](https://github.com/buildkite/agent/pull/613) (@lox)
- Skip local hooks when BUILDKITE_NO_LOCAL_HOOKS is set [\#622](https://github.com/buildkite/agent/pull/622) (@lox)

### Changed

- Bootstrap shell commands output stderr now [\#626](https://github.com/buildkite/agent/pull/626) (@lox)

## [v2.6.9](https://github.com/buildkite/agent/releases/tag/v2.6.9) (2018-01-18)
[Full Changelog](https://github.com/buildkite/agent/compare/v2.6.8...v2.6.9)

### Added

- Implement `BUILDKITE_CLEAN_CHECKOUT`, `BUILDKITE_GIT_CLONE_FLAGS` and `BUILDKITE_GIT_CLEAN_FLAGS` in bootstrap.bat [\#610](https://github.com/buildkite/agent/pull/610) (@solemnwarning)

### Fixed

- Fix unbounded memory usage in artifact uploads (#493)

## [v3.0-beta.38](https://github.com/buildkite/agent/tree/v3.0-beta.38) (2018-01-10)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.37...v3.0-beta.38)

### Fixed

- Fix bug where bootstrap with pty hangs on macOS [\#614](https://github.com/buildkite/agent/pull/614) (@lox)

## [v3.0-beta.37](https://github.com/buildkite/agent/tree/v3.0-beta.37) (2017-12-07)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.36...v3.0-beta.37)

### Fixed

- Fixed bug where agent uploads fail if no files match [\#600](https://github.com/buildkite/agent/pull/600) (@lox)
- Fixed bug where timestamps are incorrectly appended to header expansions [\#597](https://github.com/buildkite/agent/pull/597)

## [v3.0-beta.36](https://github.com/buildkite/agent/tree/v3.0-beta.36) (2017-11-23)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.35...v3.0-beta.36)

### Added

- Don't retry pipeline uploads on invalid pipelines [\#589](https://github.com/buildkite/agent/pull/589) (@DazWorrall)
- A vagrant box for windows testing [\#583](https://github.com/buildkite/agent/pull/583) (@lox)
- Binary is build with golang 1.9.2

### Fixed

- Fixed bug where malformed pipelines caused infinite loop [\#585](https://github.com/buildkite/agent/pull/585) (@lox)


## [v3.0-beta.35](https://github.com/buildkite/agent/tree/v3.0-beta.35) (2017-11-13)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.34...v3.0-beta.35)

### Added

- Support nested interpolated variables [\#578](https://github.com/buildkite/agent/pull/578) (@lox)
- Check for corrupt git repository before checkout [\#574](https://github.com/buildkite/agent/pull/574) (@lox)

### Fixed

- Fix bug where non-truthy bool arguments failed silently [\#582](https://github.com/buildkite/agent/pull/582) (@lox)
- Pass working directory changes between hooks [\#577](https://github.com/buildkite/agent/pull/577) (@lox)
- Kill cancelled tasks with taskkill on windows [\#575](https://github.com/buildkite/agent/pull/575) (@adill)
- Support hashed hosts in ssh known_hosts [\#579](https://github.com/buildkite/agent/pull/579) (@lox)

## [v3.0-beta.34](https://github.com/buildkite/agent/tree/v3.0-beta.34) (2017-10-19)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.33...v3.0-beta.34)

### Fixed

- Fix bug where pipeline upload doesn't get environment passed correctly [\#567](https://github.com/buildkite/agent/pull/567) (@lox)
- Only show "Running hook" if one exists [\#566](https://github.com/buildkite/agent/pull/566) (@lox)
- Fix segfault when using custom artifact bucket and EC2 instance role credentials [\#563](https://github.com/buildkite/agent/pull/563) (@sj26)
- Fix ssh keyscan of hosts with custom ports [\#565](https://github.com/buildkite/agent/pull/565) (@sj26)

## [v2.6.7](https://github.com/buildkite/agent/releases/tag/v2.6.7) (2017-11-13)
[Full Changelog](https://github.com/buildkite/agent/compare/v2.6.6...v2.6.7)

### Added

- Check for corrupt git repository before checkout [\#556](https://github.com/buildkite/agent/pull/556) (@lox)

### Fixed

- Kill cancelled tasks with taskkill on windows [\#571](https://github.com/buildkite/agent/pull/571) (@adill)

## [v2.6.6](https://github.com/buildkite/agent/releases/tag/v2.6.6) (2017-10-09)
[Full Changelog](https://github.com/buildkite/agent/compare/v2.6.5...v2.6.6)

### Fixed

- Backported new globbing library to fix "too many open files" during globbing [\#539](https://github.com/buildkite/agent/pull/539) (@sj26 & @lox)

## [v3.0-beta.33](https://github.com/buildkite/agent/tree/v3.0-beta.33) (2017-10-05)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.32...v3.0-beta.33)

### Added

- Interpolate env block before rest of pipeline.yml [\#552](https://github.com/buildkite/agent/pull/552) (@lox)

### Fixed

- Build hanging after git checkout [\#558](https://github.com/buildkite/agent/issues/558)

## [v3.0-beta.32](https://github.com/buildkite/agent/tree/v3.0-beta.32) (2017-09-25)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.31...v3.0-beta.32)

### Added

- Add --no-plugins option to agent [\#540](https://github.com/buildkite/agent/pull/540) (@lox)
- Support docker environment vars from v2 [\#545](https://github.com/buildkite/agent/pull/545) (@lox)

### Changed

- Refactored bootstrap to be more testable / maintainable [\#514](https://github.com/buildkite/agent/pull/514)  [\#530](https://github.com/buildkite/agent/pull/530) [\#536](https://github.com/buildkite/agent/pull/536) [\#522](https://github.com/buildkite/agent/pull/522) (@lox)
- Add BUILDKITE\_GCS\_ACCESS\_HOST for GCS Host choice [\#532](https://github.com/buildkite/agent/pull/532) (@jules2689)
- Prefer plugin, local, global and then default for hooks [\#549](https://github.com/buildkite/agent/pull/549) (@lox)
- Integration tests for v3 [\#548](https://github.com/buildkite/agent/pull/548) (@lox)
- Add docker integration tests [\#547](https://github.com/buildkite/agent/pull/547) (@lox)
- Use latest golang 1.9 [\#541](https://github.com/buildkite/agent/pull/541) (@lox)
- Faster globbing with go-zglob [\#539](https://github.com/buildkite/agent/pull/539) (@lox)
- Consolidate Environment into env package  (@lox)

### Fixed
- Fix bug where ssh-keygen error causes agent to block [\#521](https://github.com/buildkite/agent/pull/521) (@lox)
- Pre-exit hook always fires now

## [v3.0-beta.31](https://github.com/buildkite/agent/tree/v3.0-beta.31) (2017-08-14)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.30...v3.0-beta.31)

### Fixed
- Support paths in BUILDKITE\_ARTIFACT\_UPLOAD\_DESTINATION [\#519](https://github.com/buildkite/agent/pull/519) (@lox)

## [v3.0-beta.30](https://github.com/buildkite/agent/tree/v3.0-beta.30) (2017-08-11)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.29...v3.0-beta.30)

### Fixed
- Agent is prompted to verify remote server authenticity when cloning submodule from unkown host [\#503](https://github.com/buildkite/agent/issues/503)
- Windows agent cannot find git executable \(Environment variable/Path issue?\) [\#487](https://github.com/buildkite/agent/issues/487)
- ssh-keyscan doesn't work for submodules on a different host [\#411](https://github.com/buildkite/agent/issues/411)
- Fix boolean plugin config parsing [\#508](https://github.com/buildkite/agent/pull/508) (@toolmantim)

### Changed
- Stop making hook files executable [\#515](https://github.com/buildkite/agent/pull/515) (@yeungda-rea)
- Switch to yaml.v2 as the YAML parser [\#511](https://github.com/buildkite/agent/pull/511) (@keithpitt)
- Add submodule remotes to known\_hosts [\#509](https://github.com/buildkite/agent/pull/509) (@lox)

## 3.0-beta.29 - 2017-07-18

### Added
- Added a `--timestamp-lines` option to `buildkite-agent start` that will insert RFC3339 UTC timestamps at the beginning of each log line. The timestamps are not applied to header lines. [#501] (@lox)
- Ctrl-c twice will force kill the agent [#499] (@lox)
- Set the content encoding on artifacts uploaded to s3 [#494] (thanks @airhorns)
- Output fetched commit sha during git fetch for pull request [#505] (@sj26)

### Changed
- Migrate the aging goamz library to the latest aws-sdk [#474] (@lox)

## 2.6.5 - 2017-07-18
### Added
- 🔍 Output fetched commit sha during git fetch for pull request [#505]

## 3.0-beta.28 - 2017-06-23
### Added
- 🐞 The agent will now poll the AWS EC2 Tags API until it finds some tags to apply before continuing. In some cases, the agent will start and connect to Buildkite before the tags are available. The timeout for this polling can be configured with --wait-for-ec2-tags-timeout (which defaults to 10 seconds) #492

### Fixed
- 🐛 Fixed 2 Windows bugs that caused all jobs that ran through our built-in buildkite-agent bootstrap command to fail #496

## 2.6.4 - 2017-06-16
### Added
- 🚀 The buildkite-agent upstart configuration will now source /etc/default/buildkite-agent before starting the agent process. This gives you an opportunity to configure the agent outside of the standard buildkite-agent.conf file

## 3.0-beta.27 - 2017-05-31
### Added
- Allow pipeline uploads when no-command-eval is true

### Fixed
- 🐞 Fixes to a few more edge cases when exported environment variables from hooks would include additional quotes #484
- Apt server misconfigured - `Packages` reports wrong sizes/hashes
- Rewrote `export -p` parser to support multiple line env vars

## 3.0-beta.26 - 2017-05-29
### Fixed
- 🤦‍♂️ We accidentally skipped a beta version, there's no v3.0-beta.25! Doh!
- 🐛 Fixed an issue where some environment variables exported from environment hooks would have new lines appended to the end

## 3.0-beta.24 - 2017-05-26
### Added
- 🚀 Added an --append option to buildkite-agent annotate that allows you to append to the body of an existing annotation

### Fixed
- 🐛 Fixed an issue where exporting multi-line environment variables from a hook would truncate everything but the first line

## 3.0-beta.23 - 2017-05-10
### Added
- 🚀 New command buildkite-agent annotate that gives you the power to annotate a build page with information from your pipelines. This feature is currently experimental and the CLI command API may change before an official 3.0 release

## 2.6.3 - 2017-05-04
### Added
- Added support for local and global pre-exit hooks (#466)

## 3.0-beta.22 - 2017-05-04
### Added
- Renames --meta-data to --tags (#435). --meta-data will be removed in v4, and v3 versions will now show a deprecation warning.
- Fixes multiple signals not being passed to job processes (#454)
- Adds binaries for OpenBSD (#463) and DragonflyBSD (#462)
- Adds support for local and global pre-exit hooks (#465)

## 2.6.2 - 2017-05-02
### Fixed
- Backport #381 to stable: Retries for fetching EC2 metadata and tags. #461

### Added
- Add OpenBSD builds

## 2.6.1 - 2017-04-13
### Removed
- Reverted #451 as it introduced a regression. Will re-think this fix and push it out again in another release after we do some more testing

## 3.0-beta.21 - 2017-04-13
### Removed
- Reverts the changes made in #448 as it seemed to introduce a regression. We'll rethink this change and push it out in another release.

## 2.6.0 - 2017-04-13
### Fixed
- Use /bin/sh rather than /bin/bash when executing commands. This allows use in environments that don't have bash, such as Alpine Linux.

## 3.0-beta.20 - 2017-04-13
### Added
- Add plugin support for HTTP repositories with .git extensions [#447]
- Run the global environment hook before checking out plugins [#445]

### Changed
- Use /bin/sh rather than /bin/bash when executing commands. This allows use in environments that don't have bash, such as Alpine Linux. (#448)

## 3.0-beta.19 - 2017-03-29
### Added
- `buildkite-agent start --disconnect-after-job` will run the agent, and automatically disconnect after running it's first job. This has sometimes been referred to as "one shot" mode and is useful when you spin up an environment per-job and want the agent to automatically disconnect once it's finished it's job
- `buildkite-agent start --disconnect-after-job-timeout` is the time in seconds the agent will wait for that first job to be assigned. The default value is 120 seconds (2 minutes). If a job isn't assigned to the agent after this time, it will automatically disconnect and the agent process will stop.

## 3.0-beta.18 - 2017-03-27
### Fixed
- Fixes a bug where log output would get sometimes get corrupted #441


## 2.5.1 - 2017-03-27
### Fixed
- Fixes a bug where log output would get sometimes get corrupted #441

## 3.0-beta.17 - 2017-03-23
### Added
- You can now specify a custom artifact upload destination with BUILDKITE_ARTIFACT_UPLOAD_DESTINATION #421
- git clean is now performed before and after the git checkout operation #418
- Update our version of lockfile which should fixes issues with running multiple agents on the same server #428
- Fix the start script for Debian wheezy #429
- The buildkite-agent binary is now built with Golang 1.8 #433
- buildkite-agent meta-data get now supports --default flag that allows you to return a default value instead of an error if the remote key doesn't exist #440

## [2.5] - 2017-03-23
### Added
- buildkite-agent meta-data get now supports --default flag that allows you to return a default value instead of an error if the remote key doesn't exist #440

## 2.4.1 - 2017-03-20
### Fixed
- 🐞 Fixed a bug where ^^^ +++ would be prefixed with a timestamp when ---timestamp-lines was enabled #438

## [2.4] - 2017-03-07
### Added
- Added a new option --timestamp-lines option to buildkite-agent start that will insert RFC3339 UTC timestamps at the beginning of each log line. The timestamps are not applied to header lines. #430

### Changed
- Go 1.8 [#433]
- Switch to govendor for dependency tracking [#432]
- Backport Google Cloud Platform meta-data to 2.3 stable agent [#431]

## 3.0-beta.16 - 2016-12-04
### Fixed
- "No command eval" mode now makes sure commands are inside the working directory 🔐
- Scripts which are already executable won't be chmoded 🔏

## 2.3.2 - 2016-11-28
### Fixed
- 🐝 Fixed an edge case that causes the agent to panic and exit if more lines are read a process after it's finished

## 2.3.1 - 2016-11-17
### Fixed
- More resilient init.d script (#406)
- Only lock if locks are used by the system
- More explicit su with --shell option

## 3.0-beta.15 - 2016-11-16
### Changed
- The agent now receives it's "job status interval" from the Agent API (the number of seconds between checking if it's current job has been remotely canceled)

## 3.0-beta.14 - 2016-11-11
### Fixed
- Fixed a race condition where the agent would pick up another job to run even though it had been told to gracefully stop (PR #403 by @grosskur)
- Fixed path to ssh-keygen for Windows (PR #401 by @bendrucker)

## [2.3] - 2016-11-10
### Fixed
- Fixed a race condition where the agent would pick up another job to run even though it had been told to gracefully stop (PR #403 by @grosskur)

## 3.0-beta.13 - 2016-10-21
### Added
- Refactored how environment variables are interpolated in the agent
- The buildkite-agent pipeline upload command now looks for .yaml files as well
- Support for the steps.json file has been removed

## 3.0-beta.12 - 2016-10-14
### Added
- Updated buildkite-agent bootstrap for Windows so that commands won't keep running if one of them fail (similar to Bash's set -e) behaviour #392 (thanks @essen)

## 3.0-beta.11 - 2016-10-04
### Added
- AWS EC2 meta-data tagging is now more resilient and will retry on failure (#381)
- Substring expansion works for variables in pipeline uploads, like ${BUILDKITE_COMMIT:0:7} will return the first seven characters of the commit SHA (#387)

## 3.0-beta.10 - 2016-09-21
### Added
- The buildkite-agent binary is now built with Golang 1.7 giving us support for macOS Sierra
- The agent now talks HTTP2 making calls to the Agent API that little bit faster
- The binary is a statically compiled (no longer requiring libc)
- meta-data-ec2 and meta-data-ec2-tags can now be configured using BUILDKITE_AGENT_META_DATA_EC2 and BUILDKITE_AGENT_META_DATA_EC2_TAGS environment variables


## [2.2] - 2016-09-21
### Added
- The buildkite-agent binary is now built with Golang 1.7 giving us support for macOS Sierra
- The agent now talks HTTP2 making calls to the Agent API that little bit faster
- The binary is a statically compiled (no longer requiring libc)
- meta-data-ec2 and meta-data-ec2-tags can now be configured using BUILDKITE_AGENT_META_DATA_EC2 and BUILDKITE_AGENT_META_DATA_EC2_TAGS environment variables

### Changed
- We've removed our dependency of libc for greater compatibly across \*nix systems which has had a few side effects:
  We've had to remove support for changing the process title when an agent starts running a job. This feature has only ever been available to users running 64-bit ubuntu, and required us to have a dependency on libc. We'd like to bring this feature back in the future in a way that doesn't have us relying on libc
- The agent will now use Golangs internal DNS resolver instead of the one on your system. This probably won't effect you in any real way, unless you've setup some funky DNS settings for agent.buildkite.com

## 3.0-beta.9 - 2016-08-18
### Added
- Allow fetching meta-data from Google Cloud metadata #369 (Thanks so much @grosskur)

## 2.1.17 - 2016-08-11
### Fixed
- Fix some compatibility with older Git versions 🕸

## 3.0-beta.8 - 2016-08-09
### Fixed
- Make bootstrap actually use the global command hook if it exists #365

## 3.0-beta.7 - 2016-08-05
### Added
- Support plugin array configs f989cde
- Include bootstrap in the help output 7524ffb

### Fixed
- Fixed a bug where we weren't stripping ANSI colours in build log headers 6611675
- Fix Content-Type for Google Cloud Storage API calls #361 (comment)

## 2.1.16 - 2016-08-04
### Fixed
- 🔍 SSH key scanning backwards compatibility with older openssh tools

## 2.1.15 - 2016-07-28
### Fixed
- 🔍 SSH key scanning fix after it got a little broken in 2.1.14, sorry!

## 2.1.14 - 2016-07-26
### Added
- 🔍 SSH key scanning should be more resilient, whether or not you hash your known hosts file
- 🏅 Commands executed by the Bootstrap script correctly preserve positional arguments and handle interpolation better
- 🌈 ANSI color sequences are a little more resilient
- ✨ Git clean and clone flags can now be supplied in the Agent configuration file or on the command line
- 📢 Docker Compose will now be a little more verbose when the Agent is in Debug mode
- 📑 $BUILDKITE_DOCKER_COMPOSE_FILE now accepts multiple files separated by a colon (:), like $PATH

## 3.0-beta.6 - 2016-06-24
### Fixed
- Fixes to the bootstrap when using relative paths #228
- Fixed hook paths on Windows #331
- Fixed default path of the pipeline.yml file on Windows #342
- Fixed issues surrounding long command definitions #334
- Fixed default bootstrap-command command for Windows #344

## 3.0-beta.5 - 2016-06-16

## [3.0-beta.3- 2016-06-01
### Added
- Added support for BUILDKITE_GIT_CLONE_FLAGS (#330) giving you the ability customise how the agent clones your repository onto your build machines. You can use this to customise the "depth" of your repository if you want faster clones BUILDKITE_GIT_CLONE_FLAGS="-v --depth 1". This option can also be configured in your buildkite-agent.cfg file using the git-clone-flags option
- BUILDKITE_GIT_CLEAN_FLAGS can now be configured in your buildkite-agent.cfg file using the git-clean-flags option (#330)
- Allow metadata value to be read from STDIN (#327). This allows you to set meta-data from files easier cat meta-data.txt | buildkite-agent meta-data set "foo"

### Fixed
- Fixed environment variable sanitisation #333

## 2.1.13 - 2016-05-30
### Added
- BUILDKITE_GIT_CLONE_FLAGS (#326) giving you the ability customise how the agent clones your repository onto your build machines. You can use this to customise the "depth" of your repository if you want faster clones `BUILDKITE_GIT_CLONE_FLAGS="-v --depth 1"`
- Allow metadata value to be read from STDIN (#327). This allows you to set meta-data from files easier `cat meta-data.txt | buildkite-agent meta-data set "foo"`

## 3.0-beta.2 - 2016-05-23
### Fixed
- Improved error logging when failing to capture the exit status for a job (#325)

## 2.1.12 - 2016-05-23
### Fixed
- Improved error logging when failing to capture the exit status for a job (#325)

## 2.1.11 - 2016-05-17
### Added
- A new --meta-data-ec2 command line flag and config option for populating agent meta-data from EC2 information (#320)
- Binaries are now published to download.buildkite.com (#318)

## 3.0-beta.1 - 2016-05-16
### Added
- New version number: v3.0-beta.1. There will be no 2.2 (the previous beta release)
- Outputs the build directory in the build log (#317)
- Don't output the env variable values that are set from hooks (#316)
- Sign packages with SHA512 (#308)
- A new --meta-data-ec2 command line flag and config option for populating agent meta-data from EC2 information (#314)
- Binaries are now published to download.buildkite.com (#318)

## 2.2-beta.4 - 2016-05-10
### Fixed
- Amazon Linux & CentOS 6 packages now start and shutdown the agent gracefully (#306) - thanks @jnewbigin
- Build headers now work even if ANSI escape codes are applied (#279)

## 2.1.10- 2016-05-09
### Fixed
- Amazon Linux & CentOS 6 packages now start and shutdown the agent gracefully (#290 #305) - thanks @jnewbigin

## 2.1.9 - 2016-05-06
### Added
- Docker Compose 1.7.x support, including docker network removal during cleanup (#300)
- Docker Compose builds now specify --pull, so base images will always attempted to be pulled (#300)
- Docker Compose command group is now expanded by default (#300)
- Docker Compose builds now only build the specified service’s image, not all images. If you want to build all set the environment variable BUILDKITE_DOCKER_COMPOSE_BUILD_ALL=true (#297)
- Step commands are now run with bash’s -o pipefail option, preventing silent failures (#301)

### Fixed
- BUILDKITE_DOCKER_COMPOSE_LEAVE_VOLUMES undefined errors in bootstrap.sh have been fixed (#283)
- Build headers now work even if ANSI escape codes are applied

## 2.2-beta.3 - 2016-03-18
### Addeed
- Git clean brokenness has been fixed in the Go-based bootstrap (#278)

## 2.1.8- 2016-03-18
### Added
- BUILDKITE_DOCKER_COMPOSE_LEAVE_VOLUMES (#274) which allows you to keep the docker-compose volumes once a job has been run

## 2.2-beta.2 - 2016-03-17
### Added
- Environment variable substitution in pipeline files (#246)
- Google Cloud Storage for artifacts (#207)
- BUILDKITE_DOCKER_COMPOSE_LEAVE_VOLUMES (#252) which allows you to keep the docker-compose volumes once a job has been run
- BUILDKITE_S3_ACCESS_URL (#261) allowing you set your own host for build artifact links. This means you can set up your own proxy/web host that sits in front of your private S3 artifact bucket, and click directly through to them from Buildkite.
- BUILDKITE_GIT_CLEAN_FLAGS (#270) allowing you to ensure all builds have completely clean checkouts using an environment hook with export BUILDKITE_GIT_CLEAN_FLAGS=-fqdx
- Various new ARM builds (#258) allowing you to run the agent on services such as Scaleway

### Fixed
- Increased many of the HTTP timeouts to ease the stampede on the agent endpoint (#259)
- Corrected bash escaping errors which could cause problems for installs to non-standard paths (#262)
- Made HTTPS the default for all artifact upload URLs (#265)
- Added Buildkite's bin dir to the end, not the start, of $PATH (#267)
- Ensured that multiple commands separated by newlines fail as soon as a command fails (#272)

## 2.1.7- 2016-03-17
### Added
- Added support for BUILDKITE_S3_ACCESS_URL (#247) allowing you set your own host for build artifact links. This means you can set up your own proxy/web host that sits in front of your private S3 artifact bucket, and click directly through to them from Buildkite.
- Added support for BUILDKITE_GIT_CLEAN_FLAGS (#271) allowing you to ensure all builds have completely clean checkouts using an environment hook with export BUILDKITE_GIT_CLEAN_FLAGS=-fqdx
- Added support for various new ARM builds (#263) allowing you to run the agent on services such as Scaleway

### Fixed
- Updated to Golang 1.6 (26d37c5)
- Increased many of the HTTP timeouts to ease the stampede on the agent endpoint (#260)
- Corrected bash escaping errors which could cause problems for installs to non-standard paths (#266)
- Made HTTPS the default for all artifact upload URLs (#269)
- Added Buildkite's bin dir to the end, not the start, of $PATH (#268)
- Ensured that multiple commands separated by newlines fail as soon as a command fails (#273)

## 2.1.6.1 - 2016-03-09
### Fixed
- The agent is now statically linked to glibc, which means support for Debian 7 and friends (#255)

## 2.1.6 - 2016-03-03
### Fixed
- git fetch --tags doesn't fetch branches in old git (#250)

## 2.1.5 2016-02-26
### Fixed
- Use TrimPrefix instead of TrimLeft (#203)
- Update launchd templates to use .buildkite-agent dir (#212)
- Link to docker agent in README (#225)
- Send desired signal instead of always SIGTERM (#215)
- Bootstrap script fetch logic tweaks (#243)
- Avoid upstart on Amazon Linux (#244)

## 2.2-beta.1 2015-10-20
### Changed
- Added some tests to the S3Downloader

## 2.1.4 - 2015-10-16
### Fixed
- yum.buildkite.com now shows all previous versions of the agent

## 2.1.3 - 2015-10-16
### Fixed
- Fixed problem with bootstrap.sh not resetting git checkouts correctly

## 2.1.2 - 2015-10-16
### Fixed
- Removed unused functions from the bootstrap.sh file that was causing garbage output in builds
- FreeBSD 386 machines are now supported

## 2.1.1 - 2015-10-15
### Fixed
- Fixed issue with starting the bootstrap.sh file on linux systems fork/exec error

## [2.1] - 2015-10-15

## 2.1-beta.3 - 2015-10-01
### Changed
- Added support for FreeBSD - see instructions here: https://gist.github.com/keithpitt/61acb5700f75b078f199
- Only fetch the required branch + commit when running a build
- Added support for a repository command hook
- Change the git origin when a repository URL changes
- Improved mime type coverage for artefacts
- Added support for pipeline.yml files, starting to deprecate steps.json
- Show the UUID in the log output when uploading artifacts
- Added graceful shutdown #176
- Fixed header time and artifact race conditions
- OS information is now correctly collected on Windows

## 2.1-beta.2 - 2015-08-04
### Fixed
- Optimised artifact state updating
- Dump artifact upload responses when --debug-http is used

## 2.1-beta.1 - 2015-07-30
### Fixed
- Debian packages now include the debian_version property 📦
- Artifacts are uploaded faster! We've optimised our Agent API payloads to have a smaller footprint meaning you can uploading more artifacts faster! 🚗💨
- You can now download artifacts from private S3 buckets using buildkite-artifact download ☁️
- The agent will now change it's process title on linux/amd64 machines to report it's current status: `buildkite-agent v2.1 (my-agent-name) [job a4f-a4fa4-af4a34f-af4]`

## 2.1-beta - 2015-07-3

## 2.0.4 - 2015-07-2
### Fixed
- Changed the format that --version returns buildkite-agent version 2.0.4, build 456 🔍

### Added
- Added post-artifact global and local hooks 🎣

## 2.0.3.761 - 2015-07-21
### Fixed
- Debian package for ARM processors
- Include the build number in the --version call

## 2.0.3 - 2015-07-21

## 2.0.1 - 2015-07-17

## [2.0] - 2015-07-14
### Added
- The binary name has changed from buildbox to buildkite-agent
- The default install location has changed from ~/.buildbox to ~/.buildkite-agent (although each installer may install in different locations)
- Agents can be configured with a config file
- Agents register themselves with a organization-wide token, you no longer need to create them via the web
- Agents now have hooks support and there should be no reason to customise the bootstrap.sh file
- There is built-in support for containerizing builds with Docker and Docker Compose
- Windows support
- There are installer packages available for most systems
- Agents now have meta-data
- Build steps select agents using key/value patterns rather than explicit agent selection
- Automatic ssh fingerprint verification
- Ability to specify commands such as rake and make instead of a path to a script
- Agent meta data can be imported from EC2 tags
- You can set a priority for the agent
- The agent now works better under flakey internet connections by retrying certain API calls
- A new command buildkite-agent artifact shasum that allows you to download the shasum of a previously uploaded artifact
- Various bug fixes and performance enhancements
- Support for storing build pipelines in repositories

