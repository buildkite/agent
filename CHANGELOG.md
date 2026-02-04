# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic Versioning](http://semver.org/spec/v2.0.0.html).

## [v3.117.0](https://github.com/buildkite/agent/tree/v3.117.0) (2026-02-04)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.116.0...v3.117.0)

### Added
- Flag to fetch the diff-base before diffing for `if_changed` [#3689](https://github.com/buildkite/agent/pull/3689) (@DrJosh9000)

### Fixed
- Continue heartbeats while job is stopping [#3694](https://github.com/buildkite/agent/pull/3694) (@DrJosh9000)

### Internal
- Make `bucket-url` optional for cache commands [#3690](https://github.com/buildkite/agent/pull/3690) (@mitchbne)

## [v3.116.0](https://github.com/buildkite/agent/tree/v3.116.0) (2026-01-28)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.115.4...v3.116.0)

### Added
- Support checkout skipping in agent [#3672](https://github.com/buildkite/agent/pull/3672) (@mcncl)
- Add default BoolFlag, BoolTFlag values to descriptions [#3678](https://github.com/buildkite/agent/pull/3678) (@petetomasik)

### Fixed
- Exit with non-zero status if ping or heartbeat fail unrecoverably [#3687](https://github.com/buildkite/agent/pull/3687) (@DrJosh9000)
- Repeated plugins run correct number of times with always-clone-fresh [#3684](https://github.com/buildkite/agent/pull/3684) (@DrJosh9000)
- Fix nil pointer dereference in meta-data get on API timeout [#3682](https://github.com/buildkite/agent/pull/3682) (@lox)

### Changed
- In k8s mode, write BUILDKITE_ENV_FILE to /workspace [#3683](https://github.com/buildkite/agent/pull/3683) (@zhming0)

### Internal
- Refactor plugin config -> envar generation [#3655](https://github.com/buildkite/agent/pull/3655) (@moskyb)
- Dependabot updates: [#3656](https://github.com/buildkite/agent/pull/3656), [#3654](https://github.com/buildkite/agent/pull/3654), [#3662](https://github.com/buildkite/agent/pull/3662), [#3673](https://github.com/buildkite/agent/pull/3673), [#3675](https://github.com/buildkite/agent/pull/3675), [#3680](https://github.com/buildkite/agent/pull/3680), [#3681](https://github.com/buildkite/agent/pull/3681) (@dependabot[bot])

## [v3.115.4](https://github.com/buildkite/agent/tree/v3.115.4) (2026-01-13)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.115.3...v3.115.4)

### Changed

- Fallback to `/usr/bin/env bash`, when `/bin/bash` does not exist [#3661](https://github.com/buildkite/agent/pull/3661) (@sundbry), [#3667](https://github.com/buildkite/agent/pull/3667) (@zhming0)

### Internal
- Bump various container base image version. [#3669](https://github.com/buildkite/agent/pull/3669), [#3668](https://github.com/buildkite/agent/pull/3668),  [#3667](https://github.com/buildkite/agent/pull/3667) (@dependabot[bot])

## [v3.115.3](https://github.com/buildkite/agent/tree/v3.115.3) (2026-01-08)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.115.2...v3.115.3)

### Changed
- PS-1525: keep BUILDKITE_KUBERNETES_EXEC true for k8s bootstrap [#3658](https://github.com/buildkite/agent/pull/3658) (@zhming0)

### Internal
- Dependencies updates: [#3649](https://github.com/buildkite/agent/pull/3649), [#3651](https://github.com/buildkite/agent/pull/3651), [#3650](https://github.com/buildkite/agent/pull/3650), [#3648](https://github.com/buildkite/agent/pull/3648) (@dependabot[bot])


## [v3.115.2](https://github.com/buildkite/agent/tree/v3.115.2) (2025-12-18)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.115.1...v3.115.2)

### Fixed
- Try to avoid overriding BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH with false [#3644](https://github.com/buildkite/agent/pull/3644) (@DrJosh9000)
- SUP-5826: Remove experiment from 'env' command [#3635](https://github.com/buildkite/agent/pull/3635) (@Mykematt)

### Internal
- Nested-loop jitter structure for log processing [#3645](https://github.com/buildkite/agent/pull/3645) (@DrJosh9000)
- Add E2E test for Azure Blob storage [#3642](https://github.com/buildkite/agent/pull/3642) (@DrJosh9000)
- PB-1007: add e2e test for gcs artifact upload/download [#3633](https://github.com/buildkite/agent/pull/3633) (@zhming0)
- PB-1025: improve e2e test DevEX [#3634](https://github.com/buildkite/agent/pull/3634) (@zhming0)

### Dependency updates
- chore(deps): bump zstash to v0.7.0 [#3632](https://github.com/buildkite/agent/pull/3632) (@wolfeidau)
- build(deps): bump the cloud-providers group with 2 updates [#3638](https://github.com/buildkite/agent/pull/3638) (@dependabot[bot])
- build(deps): bump the otel group with 5 updates [#3637](https://github.com/buildkite/agent/pull/3637) (@dependabot[bot])
- build(deps): bump github.com/DataDog/datadog-go/v5 from 5.8.1 to 5.8.2 [#3639](https://github.com/buildkite/agent/pull/3639) (@dependabot[bot])
- build(deps): bump the container-images group across 5 directories with 1 update [#3640](https://github.com/buildkite/agent/pull/3640) (@dependabot[bot])
- build(deps): bump docker/library/golang from `cf1272d` to `54528d1` in /.buildkite in the container-images group across 1 directory [#3641](https://github.com/buildkite/agent/pull/3641) (@dependabot[bot])


## [v3.115.1](https://github.com/buildkite/agent/tree/v3.115.1) (2025-12-12)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.115.0...v3.115.1)

### Fixes
- PS-1491: Fix double retry issue for k8s mode bootstrap [#3628](https://github.com/buildkite/agent/pull/3628) (@zhming0)

### Internal
- PB-1023: remove old kubernetes bootstrap setup [#3629](https://github.com/buildkite/agent/pull/3629) (@zhming0)
- chore(deps): update zstash to v0.6.0 and update progress callback [#3630](https://github.com/buildkite/agent/pull/3630) (@wolfeidau)
- feat: add support for concurrent save and restore operations [#3627](https://github.com/buildkite/agent/pull/3627) (@wolfeidau)

## [v3.115.0](https://github.com/buildkite/agent/tree/v3.115.0) (2025-12-10)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.114.1...v3.115.0)

### Added
- `--changed-files-path` for pipeline upload, which allows users to specify a list of files changed for `if_changed` computation [#3620](https://github.com/buildkite/agent/pull/3620) (@pyrocat101)

### Fixes
- Further fixes to custom bucket artifact uploads/downloads [#3615](https://github.com/buildkite/agent/pull/3615) (@moskyb)

### Internal
- Dependabot updates [#3618](https://github.com/buildkite/agent/pull/3618) [#3619](https://github.com/buildkite/agent/pull/3619) [#3622](https://github.com/buildkite/agent/pull/3622) [#3623](https://github.com/buildkite/agent/pull/3623) [#3621](https://github.com/buildkite/agent/pull/3621) (@dependabot[bot])

## [v3.114.1](https://github.com/buildkite/agent/tree/v3.114.1) (2025-12-05)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.114.0...v3.114.1)

### Fixed
- Fix issue where artifacts uploaded to customer-managed s3 buckets could not be downloaded [#3607](https://github.com/buildkite/agent/pull/3607) (@moskyb)

### Internal
- Add an end-to-end testing framework! [#3611](https://github.com/buildkite/agent/pull/3611) [#3610](https://github.com/buildkite/agent/pull/3610) [#3609](https://github.com/buildkite/agent/pull/3609) [#3608](https://github.com/buildkite/agent/pull/3608) [#3606](https://github.com/buildkite/agent/pull/3606) [#3604](https://github.com/buildkite/agent/pull/3604) [#3599](https://github.com/buildkite/agent/pull/3599) (@DrJosh9000)
- Dependency updates [#3601](https://github.com/buildkite/agent/pull/3601) [#3600](https://github.com/buildkite/agent/pull/3600) (@dependabot[bot])
- Update MIME types [#3603](https://github.com/buildkite/agent/pull/3603) (@DrJosh9000)

## [v3.114.0](https://github.com/buildkite/agent/tree/v3.114.0) (2025-11-25)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.113.0...v3.114.0)

### Added
- feat: add agent metadata to OTEL trace attributes [#3587](https://github.com/buildkite/agent/pull/3587) (@pyrocat101)

### Fixed
- Fix for the agent sometimes failing to disconnect properly when exiting - agent pool: Send error after disconnecting [#3596](https://github.com/buildkite/agent/pull/3596) (@DrJosh9000)

### Internal
- internal/redact: Add another test with minor cleanup [#3591](https://github.com/buildkite/agent/pull/3591) (@DrJosh9000)
- Run gofumpt as part of CI [#3589](https://github.com/buildkite/agent/pull/3589) (@moskyb)

### Dependency updates
- build(deps): bump the cloud-providers group with 7 updates [#3593](https://github.com/buildkite/agent/pull/3593) (@dependabot[bot])
- build(deps): bump the container-images group across 5 directories with 1 update [#3594](https://github.com/buildkite/agent/pull/3594) (@dependabot[bot])
- build(deps): bump the container-images group across 1 directory with 2 updates [#3595](https://github.com/buildkite/agent/pull/3595) (@dependabot[bot])
- build(deps): bump golang.org/x/crypto from 0.44.0 to 0.45.0 [#3590](https://github.com/buildkite/agent/pull/3590) (@dependabot[bot])


## [v3.113.0](https://github.com/buildkite/agent/tree/v3.113.0) (2025-11-18)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.112.0...v3.113.0)

### Added
- Add Prometheus /metrics handler and some basic metrics [#3576](https://github.com/buildkite/agent/pull/3576) (@DrJosh9000)

### Fixed
- Fix the pipeline upload --reject-secrets flag not rejecting secrets [#3580](https://github.com/buildkite/agent/pull/3580) (@moskyb)
- Fix idle tracking for agents that never received jobs [#3579](https://github.com/buildkite/agent/pull/3579) (@scadu)

### Internal
- Clarify agent idlemonitor states in comment [#3582](https://github.com/buildkite/agent/pull/3582) (@DrJosh9000)
- Put secret scan error into exit message [#3581](https://github.com/buildkite/agent/pull/3581) (@DrJosh9000)

### Dependency updates
- build(deps): bump the golang-x group with 3 updates [#3583](https://github.com/buildkite/agent/pull/3583) (@dependabot[bot])
- build(deps): bump the cloud-providers group with 7 updates [#3584](https://github.com/buildkite/agent/pull/3584) (@dependabot[bot])

## [v3.112.0](https://github.com/buildkite/agent/tree/v3.112.0) (2025-11-12)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.111.0...v3.112.0)

### Added

The agent can now annotate jobs as well as builds! Job annotations will show up in a dedicated section of the job detail
in the build UI. This is a great way to provide additional, richly-formatted context and information about specific jobs.

See the [PR](https://github.com/buildkite/agent/pull/3569) for more details.

### Changed
- Agents will now check for new work more quickly immediately after finishing a job [#3571](https://github.com/buildkite/agent/pull/3571) (@DrJosh9000)

### Fixed
- IdleMonitor-related fixes [#3570](https://github.com/buildkite/agent/pull/3570) (@DrJosh9000)
- Fix confusing error message when hashing artifact payloads [#3565](https://github.com/buildkite/agent/pull/3565) (@moskyb)

### Internal
- Dependency updates [#3575](https://github.com/buildkite/agent/pull/3575) [#3574](https://github.com/buildkite/agent/pull/3574) [#3573](https://github.com/buildkite/agent/pull/3573) [#3572](https://github.com/buildkite/agent/pull/3572) (@dependabot[bot])

## [v3.111.0](https://github.com/buildkite/agent/tree/v3.111.0) (2025-11-05)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.110.0...v3.111.0)

> [!WARNING]
> If you use a custom S3 bucket for artifacts, this applies to you.
>
> As part of updating to AWS Go SDK v2, the "credential chain" for providing
> authentication credentials to access artifacts in custom S3 buckets, is now
> more standard. The existing `BUILDKITE_S3_` env vars are still available and
> take precedence, but when these are not set, the AWS-default mechanisms are
> used as provided by the SDK, with as few customisations as possible.
>
> This means additional ways to pass credentials to the AWS S3 client may be
> accepted, and where multiple credentials are available, the precedence may
> have changed (to match what the AWS SDK expects by default).
>
> Because of this, and the number of combinations of different ways to provide
> credentials, this change may inadvertently break pipelines using custom S3
> buckets for artifacts. Please reach out to support@buildkite.com or raise
> issues in GitHub if this impacts you!

### Added
- Add cache save and restore using github.com/buildkite/zstash [#3551](https://github.com/buildkite/agent/pull/3551) (@wolfeidau)

### Changed
- Upgrade to AWS Go SDK v2 [#3554](https://github.com/buildkite/agent/pull/3554) (@DrJosh9000)
- Catch all 'ignored' vars [#3502](https://github.com/buildkite/agent/pull/3502) (@DrJosh9000)

### Internal
- chore: go modernize to do a bit of a tidy up and remove some junk [#3560](https://github.com/buildkite/agent/pull/3560) (@wolfeidau)
- Enforce that command descriptions indent using spaces, not tabs [#3553](https://github.com/buildkite/agent/pull/3553) (@moskyb)

### Dependency updates
- build(deps): bump the cloud-providers group across 1 directory with 9 updates [#3566](https://github.com/buildkite/agent/pull/3566) (@dependabot[bot])
- build(deps): bump golangci/golangci-lint from v2.5-alpine to v2.6-alpine in /.buildkite in the container-images group across 1 directory [#3563](https://github.com/buildkite/agent/pull/3563) (@dependabot[bot])
- build(deps): bump the container-images group across 4 directories with 1 update [#3564](https://github.com/buildkite/agent/pull/3564) (@dependabot[bot])
- build(deps): bump gopkg.in/DataDog/dd-trace-go.v1 from 1.74.7 to 1.74.8 [#3555](https://github.com/buildkite/agent/pull/3555) (@dependabot[bot])
- build(deps): bump the cloud-providers group with 6 updates [#3556](https://github.com/buildkite/agent/pull/3556) (@dependabot[bot])
- build(deps): bump the container-images group across 4 directories with 1 update [#3557](https://github.com/buildkite/agent/pull/3557) (@dependabot[bot])
- build(deps): bump docker/library/golang from `02ce1d7` to `5034fa4` in /.buildkite in the container-images group across 1 directory [#3558](https://github.com/buildkite/agent/pull/3558) (@dependabot[bot])


## [v3.110.0](https://github.com/buildkite/agent/tree/v3.110.0) (2025-10-22)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.109.1...v3.110.0)

### Added
- Configurable chunks interval [#3521](https://github.com/buildkite/agent/pull/3521) (@catkins)
- Inject OpenTelemetry context to all child processes [#3548](https://github.com/buildkite/agent/pull/3548) (@zhming0)
  - This is done using [environment variables](https://opentelemetry.io/docs/specs/otel/context/env-carriers/). This may interfere with existing OTel environment variables if they are manually added some other way.
- Add --literal and --delimiter flags to artifact upload [#3543](https://github.com/buildkite/agent/pull/3543) (@DrJosh9000)

### Changed
Various improvements and fixes to do with signal and cancel grace periods, and signal handling, most notably:
- When cancelling a job, the timeout before sending a SIGKILL to the job has changed from cancel-grace-period to signal-grace-period (`--signal-grace-period-seconds` flag, `BUILDKITE_SIGNAL_GRACE_PERIOD_SECONDS` env var) to allow the agent some extra time to upload job logs and mark the job as finished. By default, signal-grace-period is 1 second shorter than cancel-grace-period. You may wish to increase cancel-grace-period accordingly.
- When SIGQUIT is handled by the bootstrap, the exit code is now 131, and it no longer dumps a stacktrace.
- The recently-added `--kubernetes-log-collection-grace-period` flag is now deprecated. Instead, use `--cancel-grace-period`.
- When running the agent interactively, you can now Ctrl-C a third time to exit immediately.
- In Kubernetes mode, the agent now begins shutting down on the first SIGTERM. The kubernetes-bootstrap now swallows SIGTERM with a logged message, and waits for the agent container to send an interrupt.
- When the agent is cancelling jobs because it is stopping, all jobs start cancellation simultaneously. This allows the agent to exit sooner when multiple workers (`--spawn` flag) are used.
See [#3549](https://github.com/buildkite/agent/pull/3549), [#3547](https://github.com/buildkite/agent/pull/3547), [#3534](https://github.com/buildkite/agent/pull/3534) (@DrJosh9000)

### Fixed
- Refresh checkout root file handle after checkout hook [#3546](https://github.com/buildkite/agent/pull/3546) (@zhming0)
- Bump zzglob to v0.4.2 to fix uploading artifact paths containing `~` [#3539](https://github.com/buildkite/agent/pull/3539) (@DrJosh9000)

### Internal
- Docs: Add examples for step update commands for priority and notify attributes [#3532](https://github.com/buildkite/agent/pull/3532) (@tomowatt)
- Docs: Update URLs in agent cfg comments [#3536](https://github.com/buildkite/agent/pull/3536) (@petetomasik)

### Dependency updates
- Upgrade Datadog-go to v5.8.1 to work around mod checksum issues [#3538](https://github.com/buildkite/agent/pull/3538) (@dannyfallon)
- build(deps): bump the container-images group across 3 directories with 2 updates [#3545](https://github.com/buildkite/agent/pull/3545) (@dependabot[bot])
- build(deps): bump gopkg.in/DataDog/dd-trace-go.v1 from 1.74.6 to 1.74.7 [#3544](https://github.com/buildkite/agent/pull/3544) (@dependabot[bot])
- build(deps): bump github.com/gofrs/flock from 0.12.1 to 0.13.0 [#3523](https://github.com/buildkite/agent/pull/3523) (@dependabot[bot])
- build(deps): bump docker/library/golang from 1.24.8 to 1.24.9 in /.buildkite in the container-images group across 1 directory [#3542](https://github.com/buildkite/agent/pull/3542) (@dependabot[bot])
- build(deps): bump the cloud-providers group across 1 directory with 6 updates [#3541](https://github.com/buildkite/agent/pull/3541) (@dependabot[bot])
- build(deps): bump the container-images group across 3 directories with 1 update [#3540](https://github.com/buildkite/agent/pull/3540) (@dependabot[bot])
- build(deps): bump the golang-x group with 5 updates [#3525](https://github.com/buildkite/agent/pull/3525) (@dependabot[bot])


## [v3.109.1](https://github.com/buildkite/agent/tree/v3.109.1) (2025-10-15)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.109.0...v3.109.1)

### Fixed
- Pass aws config to ec2 client for fetching tags [#3529](https://github.com/buildkite/agent/pull/3529) (@migueleliasweb)
- PS-1245: Fix artifact search output format escape sequence handling [#3522](https://github.com/buildkite/agent/pull/3522) (@zhming0)
- Fix inconsistency in artifact search --format flag documentation [#3520](https://github.com/buildkite/agent/pull/3520) (@ivannalisetska)

## [v3.109.0](https://github.com/buildkite/agent/tree/v3.109.0) (2025-10-09)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.108.0...v3.109.0)

### Added
- if_changed: support lists, include/exclude [#3518](https://github.com/buildkite/agent/pull/3518) (@DrJosh9000)

### Fixed
- Improve if_changed when base=HEAD [#3510](https://github.com/buildkite/agent/pull/3510) (@DrJosh9000)

### Internal
- Update EC2 tags/metadata to use AWS Go SDK v2 [#3434](https://github.com/buildkite/agent/pull/3434) (@DrJosh9000)

### Dependency updates
- build(deps): bump golang.org/x/net from 0.44.0 to 0.45.0 in the golang-x group [#3516](https://github.com/buildkite/agent/pull/3516) (@dependabot[bot])
- build(deps): bump the cloud-providers group with 2 updates [#3517](https://github.com/buildkite/agent/pull/3517) (@dependabot[bot])
- build(deps): bump docker/library/golang from 1.24.7 to 1.24.8 in /.buildkite in the container-images group across 1 directory [#3515](https://github.com/buildkite/agent/pull/3515) (@dependabot[bot])
- build(deps): bump the cloud-providers group with 2 updates [#3511](https://github.com/buildkite/agent/pull/3511) (@dependabot[bot])
- build(deps): bump docker/library/golang from `87916ac` to `2c5f7a0` in /.buildkite in the container-images group across 1 directory [#3513](https://github.com/buildkite/agent/pull/3513) (@dependabot[bot])
- build(deps): bump drjosh.dev/zzglob from 0.4.0 to 0.4.1 [#3512](https://github.com/buildkite/agent/pull/3512) (@dependabot[bot])
- build(deps): bump the container-images group across 5 directories with 1 update [#3514](https://github.com/buildkite/agent/pull/3514) (@dependabot[bot])


## [v3.108.0](https://github.com/buildkite/agent/tree/v3.108.0) (2025-10-02)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.107.2...v3.108.0)

### Added
- Ability to checkout subdirectories of Plugins [#3488](https://github.com/buildkite/agent/pull/3488) (@tomowatt)
- Better env var for disabling if_changed [#3501](https://github.com/buildkite/agent/pull/3501) (@DrJosh9000)

### Fixed
- Fix log collection stopping too early on SIGTERM in Kubernetes [#3500](https://github.com/buildkite/agent/pull/3500) (@scadu)
- Update gopsutils to 4.25.8 [#3499](https://github.com/buildkite/agent/pull/3499) (@ladd)

### Dependency updates
- build(deps): bump the container-images group across 5 directories with 1 update [#3505](https://github.com/buildkite/agent/pull/3505) (@dependabot[bot])
- build(deps): bump github.com/DataDog/datadog-go/v5 from 5.6.0 to 5.8.0 [#3504](https://github.com/buildkite/agent/pull/3504) (@dependabot[bot])
- build(deps): bump cloud.google.com/go/compute/metadata from 0.8.4 to 0.9.0 [#3506](https://github.com/buildkite/agent/pull/3506) (@dependabot[bot])
- build(deps): bump the cloud-providers group with 5 updates [#3503](https://github.com/buildkite/agent/pull/3503) (@dependabot[bot])


## [v3.107.2](https://github.com/buildkite/agent/tree/v3.107.2) (2025-09-24)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.107.1...v3.107.2)

### Fixed
- Remove debugging log line [#3496](https://github.com/buildkite/agent/pull/3496) (@DrJosh9000)

## [v3.107.1](https://github.com/buildkite/agent/tree/v3.107.1) (2025-09-24)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.107.0...v3.107.1)

### Added
- Add plugins-always-clone-fresh to config, CLI start [#3429](https://github.com/buildkite/agent/pull/3429) (@petetomasik)

### Fixed
- Set e.checkoutRoot even if checkout phase is disabled [#3493](https://github.com/buildkite/agent/pull/3493) (@DrJosh9000)

### Internal
- Simplify secret tests [#3484](https://github.com/buildkite/agent/pull/3484) (@moskyb)

### Dependency updates
- build(deps): bump rexml from 3.3.9 to 3.4.2 [#3494](https://github.com/buildkite/agent/pull/3494) (@dependabot[bot])
- build(deps): bump cloud.google.com/go/compute/metadata from 0.8.0 to 0.8.4 [#3489](https://github.com/buildkite/agent/pull/3489) (@dependabot[bot])
- build(deps): bump the cloud-providers group across 1 directory with 3 updates [#3490](https://github.com/buildkite/agent/pull/3490) (@dependabot[bot])
- build(deps): bump the container-images group across 4 directories with 1 update [#3491](https://github.com/buildkite/agent/pull/3491) (@dependabot[bot])
- build(deps): bump the container-images group across 1 directory with 2 updates [#3492](https://github.com/buildkite/agent/pull/3492) (@dependabot[bot])


## [v3.107.0](https://github.com/buildkite/agent/tree/v3.107.0) (2025-09-18)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.106.0...v3.107.0)

### Added
- Added ability to fetch multiple secrets in a single call [#3483](https://github.com/buildkite/agent/pull/3483) (@moskyb)
- Experiment for propagating agent config env vars [#3471](https://github.com/buildkite/agent/pull/3471) (@DrJosh9000)
- `oidc request-token` can now output in a GCP Workload Federation-compatible format [#3480](https://github.com/buildkite/agent/pull/3480) (@moskyb)

### Changed
- Update docs for apply-if-changed information with agent minimum version [#3485](https://github.com/buildkite/agent/pull/3485) (@Damilola-obasa)

### Internal
- Use the go.mod tool block for more tools [#3481](https://github.com/buildkite/agent/pull/3481) (@DrJosh9000)
- Update shellwords to v1.0.1, relax Go version directive [#3464](https://github.com/buildkite/agent/pull/3464) (@moskyb)
- build(deps): bump the container-images group across 5 directories with 1 update [#3478](https://github.com/buildkite/agent/pull/3478) (@dependabot[bot])
- Split Dependabot container updates [#3477](https://github.com/buildkite/agent/pull/3477) (@DrJosh9000)

## [v3.106.0](https://github.com/buildkite/agent/tree/v3.106.0) (2025-09-15)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.105.0...v3.106.0)

### Changed
- Support upcoming `secrets` pipeline syntax (currently in private preview) [#3453](https://github.com/buildkite/agent/pull/3453) (@matthewborden)
- Better plugin and hook path checks [#3445](https://github.com/buildkite/agent/pull/3445) (@DrJosh9000)

## [v3.105.0](https://github.com/buildkite/agent/tree/v3.105.0) (2025-09-11)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.104.0...v3.105.0)


### Fixed
- PS-1101: refresh Executor config for Job API env change in polyglot hook [#3467](https://github.com/buildkite/agent/pull/3467) (@zhming0)
- PB-610: fix hook environment variable unable to propagate via bk-agent env set [#3466](https://github.com/buildkite/agent/pull/3466) (@zhming0)

### Added
- Support agent checkout on pull request merge refspecs [#3436](https://github.com/buildkite/agent/pull/3436) (@jonathanly)

### Internal
- Lower Go containers back to 1.24 [#3468](https://github.com/buildkite/agent/pull/3468) (@DrJosh9000)
- Add replacer fuzz test corpus to repo, with fix [#3448](https://github.com/buildkite/agent/pull/3448) (@DrJosh9000)
- Re-add test race detection, and skip a known-racy test under the race  regime [#3452](https://github.com/buildkite/agent/pull/3452) (@moskyb)
- Dependancy updates: [#3463](https://github.com/buildkite/agent/pull/3463), [#3465](https://github.com/buildkite/agent/pull/3465), [#3462](https://github.com/buildkite/agent/pull/3462) ,[#3457](https://github.com/buildkite/agent/pull/3457), [#3460](https://github.com/buildkite/agent/pull/3460), [#3456](https://github.com/buildkite/agent/pull/3456), [#3454](https://github.com/buildkite/agent/pull/3454) (@dependabot[bot])


## [v3.104.0](https://github.com/buildkite/agent/tree/v3.104.0) (2025-09-05)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.103.1...v3.104.0)

### Security
- Automatically redact OIDC tokens in logs [#3450](https://github.com/buildkite/agent/pull/3450) (@matthewborden)

### Added
- Allow multiple pipeline files for upload [#3431](https://github.com/buildkite/agent/pull/3431) (@DrJosh9000)

### Changed
- Promote use-zzglob experiment to default [#3428](https://github.com/buildkite/agent/pull/3428) (@DrJosh9000)

### Fixed
- Ensure bootstrap waits for signal propagation before exiting [#3443](https://github.com/buildkite/agent/pull/3443) (@moskyb)
- Fix experiment promotion message [#3432](https://github.com/buildkite/agent/pull/3432) (@DrJosh9000)

### Internal
- Add disclosures/credits to PR template [#3433](https://github.com/buildkite/agent/pull/3433) (@DrJosh9000)
- Fix code owners [#3422](https://github.com/buildkite/agent/pull/3422) (@zhming0)
- Dependency updates [#3437](https://github.com/buildkite/agent/pull/3437), [#3438](https://github.com/buildkite/agent/pull/3438), [#3442](https://github.com/buildkite/agent/pull/3442), [#3441](https://github.com/buildkite/agent/pull/3441), [#3435](https://github.com/buildkite/agent/pull/3435), [#3425](https://github.com/buildkite/agent/pull/3425), [#3423](https://github.com/buildkite/agent/pull/3423), [#3426](https://github.com/buildkite/agent/pull/3426), [#3427](https://github.com/buildkite/agent/pull/3427), [#3424](https://github.com/buildkite/agent/pull/3424) (@dependabot[bot])


## [v3.103.1](https://github.com/buildkite/agent/tree/v3.103.1) (2025-08-07)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.103.0...v3.103.1)

### Fixed
- PS-980: fix custom TMPDIR break hook wrapper [#3416](https://github.com/buildkite/agent/pull/3416) (@zhming0)

### Changed
- PS-1000: ensure a static & short checkout path for k8s stack agent [#3420](https://github.com/buildkite/agent/pull/3420) (@zhming0)
- Make the 'Pipeline upload not yet applied: processing' message info, not warning [#3419](https://github.com/buildkite/agent/pull/3419) (@moskyb)

### Internal
- build(deps): bump thor from 0.19.4 to 1.4.0 [#3417](https://github.com/buildkite/agent/pull/3417) (@dependabot[bot])
- build(deps): bump the cloud-providers group across 1 directory with 7 updates [#3414](https://github.com/buildkite/agent/pull/3414) (@dependabot[bot])
- build(deps): bump the container-images group across 7 directories with 4 updates [#3415](https://github.com/buildkite/agent/pull/3415) (@dependabot[bot])
- Update to use OIDC session tags on AWS role assumption [#3412](https://github.com/buildkite/agent/pull/3412) (@duckalini)
- chore: move the tool.go to new tool dependency [#3409](https://github.com/buildkite/agent/pull/3409) (@wolfeidau)
- Upgrade to go-pipeline v0.15.0 [#3408](https://github.com/buildkite/agent/pull/3408) (@DrJosh9000)
- Only run tests if code has changed [#3407](https://github.com/buildkite/agent/pull/3407) (@DrJosh9000)


## [v3.103.0](https://github.com/buildkite/agent/tree/v3.103.0) (2025-07-22)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.102.2...v3.103.0)

### Added
- Configurable kubernetes-bootstrap connection timeout [#3398](https://github.com/buildkite/agent/pull/3398) (@zhming0)

### Changed
- Exit with a specific code when the Job being Acquired is Locked [#3403](https://github.com/buildkite/agent/pull/3403) (@CerealBoy)
- Rename local -> repository hooks, global -> agent hooks [#3401](https://github.com/buildkite/agent/pull/3401) (@moskyb)
- Use `BUILDKITE_PIPELINE_DEFAULT_BRANCH` as a default git diff base [#3396](https://github.com/buildkite/agent/pull/3396) (@DrJosh9000)
- `apply-if-changed` now enabled by default - `if_changed` improvements [#3387](https://github.com/buildkite/agent/pull/3387) (@DrJosh9000)

### Internal
- Update to use OIDC session tokens on AWS role assumption [#3395](https://github.com/buildkite/agent/pull/3395) (@duckalini)
- Annotate with lint findings [#3404](https://github.com/buildkite/agent/pull/3404) (@DrJosh9000)
- Lint fixes [#3383](https://github.com/buildkite/agent/pull/3383), [#3399](https://github.com/buildkite/agent/pull/3399) (@DrJosh9000)

### Dependencies
- build(deps): bump the cloud-providers group with 5 updates [#3406](https://github.com/buildkite/agent/pull/3406) (@dependabot[bot])
- build(deps): bump the container-images group across 6 directories with 2 updates [#3405](https://github.com/buildkite/agent/pull/3405) (@dependabot[bot])
- build(deps): bump the golang-x group with 4 updates [#3391](https://github.com/buildkite/agent/pull/3391) (@dependabot[bot])
- build(deps): bump google.golang.org/api from 0.240.0 to 0.241.0 in the cloud-providers group [#3389](https://github.com/buildkite/agent/pull/3389) (@dependabot[bot])
- build(deps): bump the container-images group across 6 directories with 3 updates [#3390](https://github.com/buildkite/agent/pull/3390) (@dependabot[bot])
- build(deps): bump gopkg.in/DataDog/dd-trace-go.v1 from 1.74.2 to 1.74.3 [#3388](https://github.com/buildkite/agent/pull/3388) (@dependabot[bot])

## [v3.102.2](https://github.com/buildkite/agent/tree/v3.102.2) (2025-07-15)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.102.1...v3.102.2)

### Changed
- Fix to reflect-exit-status flag [#3393](https://github.com/buildkite/agent/pull/3393) (@DrJosh9000)

## [v3.102.1](https://github.com/buildkite/agent/tree/v3.102.1) (2025-07-14)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.102.0...v3.102.1)

### Fixed
- Add reflect-exit-status flag to control exiting with job status [#3385](https://github.com/buildkite/agent/pull/3385) (@DrJosh9000)
- Normalise indentation in redactor add usage [#3382](https://github.com/buildkite/agent/pull/3382) (@DrJosh9000)

## [v3.102.0](https://github.com/buildkite/agent/tree/v3.102.0) (2025-07-09)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.101.0...v3.102.0)

### Added
- Add disconnect-after-uptime flag to set a max lifetime for agents [#3370](https://github.com/buildkite/agent/pull/3370) (@nosammai)

### Changed
- Exit with same code in acquire-job mode [#3376](https://github.com/buildkite/agent/pull/3376) (@DrJosh9000)

### Fixed
- Fix git mirrors + refspec [#3381](https://github.com/buildkite/agent/pull/3381) (@sj26)
- Print valid JSON in log output [#3374](https://github.com/buildkite/agent/pull/3374) (@ChrisBr)
- Adding a reference in our docs the limit of an annotation's contexts [#3261](https://github.com/buildkite/agent/pull/3261) (@lizrabuya)
- docs redactor clarify multi-secret JSON usage and limit [#3343](https://github.com/buildkite/agent/pull/3343) (@ivannalisetska)

### Internal
- Update homebrew formula location [#3375](https://github.com/buildkite/agent/pull/3375) (@sj26)

### Dependencies
- build(deps): bump the container-images group across 6 directories with 2 updates [#3379](https://github.com/buildkite/agent/pull/3379) (@dependabot[bot])
- build(deps): bump google.golang.org/api from 0.239.0 to 0.240.0 in the cloud-providers group [#3377](https://github.com/buildkite/agent/pull/3377) (@dependabot[bot])
- build(deps): bump the container-images group across 7 directories with 3 updates [#3378](https://github.com/buildkite/agent/pull/3378) (@dependabot[bot])

## [v3.101.0](https://github.com/buildkite/agent/tree/v3.101.0) (2025-07-01)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.100.1...v3.101.0)

### Added
- Add support for http/protobuf transport for OTLP [#3366](https://github.com/buildkite/agent/pull/3366) (@catkins)

### Fixed
- Tweak apply-if-changed flag and usage string [#3367](https://github.com/buildkite/agent/pull/3367) (@DrJosh9000)
- Gather changed files list once [#3368](https://github.com/buildkite/agent/pull/3368) (@DrJosh9000)
- if_changed fixes: support older Git versions, adhere to skip string limit [#3372](https://github.com/buildkite/agent/pull/3372) (@DrJosh9000)
- Self-execute the path from os.Executable in more places [#3338](https://github.com/buildkite/agent/pull/3338) (@DrJosh9000)

### Dependencies
- build(deps): bump the otel group with 9 updates [#3362](https://github.com/buildkite/agent/pull/3362) (@dependabot[bot])
- build(deps): bump the cloud-providers group with 2 updates [#3363](https://github.com/buildkite/agent/pull/3363) (@dependabot[bot])
- build(deps): bump the container-images group across 6 directories with 2 updates [#3364](https://github.com/buildkite/agent/pull/3364) (@dependabot[bot])
- build(deps): bump gopkg.in/DataDog/dd-trace-go.v1 from 1.74.0 to 1.74.2 [#3365](https://github.com/buildkite/agent/pull/3365) (@dependabot[bot])
- build(deps): bump github.com/go-viper/mapstructure/v2 from 2.2.1 to 2.3.0 [#3371](https://github.com/buildkite/agent/pull/3371) (@dependabot[bot])

## [v3.100.1](https://github.com/buildkite/agent/tree/v3.100.1) (2025-06-25)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.100.0...v3.100.1)

### Fixed
- Fix regression in pipeline upload with no-interpolation [#3359](https://github.com/buildkite/agent/pull/3359) (@DrJosh9000)

### Internal
- Avoid goroutine failing after test [#3356](https://github.com/buildkite/agent/pull/3356) (@DrJosh9000)

### Dependencies
- build(deps): bump github.com/buildkite/shellwords from 0.0.0-20180315084142-c3f497d1e000 to 1.0.0 [#3352](https://github.com/buildkite/agent/pull/3352) (@dependabot[bot])
- build(deps): bump github.com/go-chi/chi/v5 from 5.2.1 to 5.2.2 [#3353](https://github.com/buildkite/agent/pull/3353) (@dependabot[bot])
- build(deps): bump the container-images group across 6 directories with 2 updates [#3354](https://github.com/buildkite/agent/pull/3354) (@dependabot[bot])
- build(deps): bump the cloud-providers group with 5 updates [#3355](https://github.com/buildkite/agent/pull/3355) (@dependabot[bot])

## [v3.100.0](https://github.com/buildkite/agent/tree/v3.100.0) (2025-06-23)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.99.0...v3.100.0)

### Fixed
- PS-794: fix vendored plugin path ending with slash breaking envvar names [#3346](https://github.com/buildkite/agent/pull/3346) (@zhming0)

### Added
- [PIPE-1021] Propagate parent OTel trace/span from backend if provided [#3348](https://github.com/buildkite/agent/pull/3348) (@catkins)

## [v3.99.0](https://github.com/buildkite/agent/tree/v3.99.0) (2025-06-20)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.98.2...v3.99.0)

### Fixed
- Fix AquireJob to return early and trigger a sentinal error for rejection [#3349](https://github.com/buildkite/agent/pull/3349) (@wolfeidau)
- Upload all pipelines present in the input [#3347](https://github.com/buildkite/agent/pull/3347) (@DrJosh9000)
- Add if_changed processing to pipeline upload [#3226](https://github.com/buildkite/agent/pull/3226) (@DrJosh9000)

> [!IMPORTANT]
> This includes a fix for a regression agent behavior, AcquireJob which no longer reports "non eligible" jobs with a exit code 27.

## [v3.98.2](https://github.com/buildkite/agent/tree/v3.98.2) (2025-06-17)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.98.1...v3.98.2)

### Fixed
- Fix PR ref retry logic [#3339](https://github.com/buildkite/agent/pull/3339) (@moskyb)
- Add stack_error signal reason [#3332](https://github.com/buildkite/agent/pull/3332) (@moskyb)
- Better helptext [#3334](https://github.com/buildkite/agent/pull/3334) (@moskyb)
- Update CLI cancel_signal arg description [#3325](https://github.com/buildkite/agent/pull/3325) (@petetomasik)

### Internal
- Dependency updates [#3342](https://github.com/buildkite/agent/pull/3342) [#3341](https://github.com/buildkite/agent/pull/3341) [#3340](https://github.com/buildkite/agent/pull/3340) [#3336](https://github.com/buildkite/agent/pull/3336) [#3337](https://github.com/buildkite/agent/pull/3337) [#3335](https://github.com/buildkite/agent/pull/3335) (@dependabot[bot])

## [v3.98.1](https://github.com/buildkite/agent/tree/v3.98.1) (2025-06-04)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.98.0...v3.98.1)

### Fixed
- Gracefully Handle Missing GitHub PR refs/pull/%s/head in Checkout [#3294](https://github.com/buildkite/agent/pull/3294) (@123sarahj123)
- Fix bootstrap subprocess handling [#3331](https://github.com/buildkite/agent/pull/3331) (@DrJosh9000)
- Reduce git fetch from twice to once for typical Github PR build [#3327](https://github.com/buildkite/agent/pull/3327) (@zhming0)
- Set job log tempfile permissions to 644 (was 600) [#3330](https://github.com/buildkite/agent/pull/3330) (@moskyb)

### Internal
- Tag tests with os / arch [#3326](https://github.com/buildkite/agent/pull/3326) (@catkins)

## [v3.98.0](https://github.com/buildkite/agent/tree/v3.98.0) (2025-05-27)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.97.2...v3.98.0)

### Added
- Add build URL to log fields [#3317](https://github.com/buildkite/agent/pull/3317) (@ChrisBr)
- Add kubernetes-bootstrap subcommand [#3306](https://github.com/buildkite/agent/pull/3306), [#3314](https://github.com/buildkite/agent/pull/3314), [#3316](https://github.com/buildkite/agent/pull/3316) (@DrJosh9000)

### Fixed
- Fix `redactor add --format json` help string [#3322](https://github.com/buildkite/agent/pull/3322) (@francoiscampbell)

## Dependency updates
- [#3320](https://github.com/buildkite/agent/pull/3320), [#3318](https://github.com/buildkite/agent/pull/3318), [#3319](https://github.com/buildkite/agent/pull/3319), [#3323](https://github.com/buildkite/agent/pull/3323), [#3321](https://github.com/buildkite/agent/pull/3321) (@dependabot[bot])


## [v3.97.2](https://github.com/buildkite/agent/tree/v3.97.2) (2025-05-13)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.97.1...v3.97.2)

### Fixed
- fix: Don't disconnect-after-idle when just given a job [#3312](https://github.com/buildkite/agent/pull/3312) (@DrJosh9000)

### Dependency updates
- [#3307](https://github.com/buildkite/agent/pull/3307), [#3311](https://github.com/buildkite/agent/pull/3311), [#3308](https://github.com/buildkite/agent/pull/3308), [#3309](https://github.com/buildkite/agent/pull/3309), [#3310](https://github.com/buildkite/agent/pull/3310) (@dependabot[bot])


## [v3.97.1](https://github.com/buildkite/agent/tree/v3.97.1) (2025-05-12)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.97.0...v3.97.1)

### Fixed
- Fix unusable `BUILDKITE_AGENT_TAGS_FROM_EC2_TAGS` env var [#3285](https://github.com/buildkite/agent/pull/3285) (@shanesmith)
- Set ignore_agent_in_dispatches when finishing with disconnect-after-job [#3297](https://github.com/buildkite/agent/pull/3297) (@DrJosh9000)

### Internal
- Introduce a structure where coverage can increase on githttp checkout code [#3296](https://github.com/buildkite/agent/pull/3296) (@wolfeidau)
- TE-3708-follow-up: Use go test -cover to generate coverage report [#3295](https://github.com/buildkite/agent/pull/3295) (@zhming0)
- TE-3708: use bktec on agent [#3292](https://github.com/buildkite/agent/pull/3292) (@zhming0)

### Dependency updates
- [#3298](https://github.com/buildkite/agent/pull/3298), [#3300](https://github.com/buildkite/agent/pull/3300), [#3301](https://github.com/buildkite/agent/pull/3301), [#3299](https://github.com/buildkite/agent/pull/3299), [#3287](https://github.com/buildkite/agent/pull/3287), [#3290](https://github.com/buildkite/agent/pull/3290), [#3291](https://github.com/buildkite/agent/pull/3291) (@dependabot[bot])

## [v3.97.0](https://github.com/buildkite/agent/tree/v3.97.0) (2025-04-16)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.96.0...v3.97.0)

### Added
- `api.Client` sends request headers specified by server in register & ping [#3268](https://github.com/buildkite/agent/pull/3268) (@pda)

### Fixed
- Ignore the ping interval if agent will disconnect after job [#3282](https://github.com/buildkite/agent/pull/3282) (@patrobinson)
- fix: keep fetching status after interrupt [#3277](https://github.com/buildkite/agent/pull/3277) (@DrJosh9000)

### Internal
- chore: flag uniformity through embedding [#3276](https://github.com/buildkite/agent/pull/3276) (@DrJosh9000)
- locally cache nginx mime types [#3284](https://github.com/buildkite/agent/pull/3284) (@patrobinson)

### Dependency updates
- build(deps): bump the container-images group across 5 directories with 3 updates [#3280](https://github.com/buildkite/agent/pull/3280) (@dependabot[bot])
- build(deps): bump the cloud-providers group across 1 directory with 4 updates [#3281](https://github.com/buildkite/agent/pull/3281) (@dependabot[bot])
- build(deps): bump golang.org/x/net from 0.38.0 to 0.39.0 in the golang-x group [#3278](https://github.com/buildkite/agent/pull/3278) (@dependabot[bot])

## [v3.96.0](https://github.com/buildkite/agent/tree/v3.96.0) (2025-04-10)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.95.1...v3.96.0)

### Added
- Add pause and resume subcommands [#3273](https://github.com/buildkite/agent/pull/3273) (@DrJosh9000)

### Internal
- chore: Use golangci-lint for code checks [#3274](https://github.com/buildkite/agent/pull/3274) (@DrJosh9000)
- `FakeAPIServer`'s `PingHandler` is passed the `*http.Request` [#3271](https://github.com/buildkite/agent/pull/3271) (@pda)
- `FakeAPIServer` handles agent registration: `AddRegistration(tok, resp)` [#3272](https://github.com/buildkite/agent/pull/3272) (@pda)
- fix: ISE message when json.Marshal fails [#3270](https://github.com/buildkite/agent/pull/3270) (@DrJosh9000)
- agent_worker_test: tests for endpoint switching during register/ping [#3269](https://github.com/buildkite/agent/pull/3269) (@pda)
- Refactor fake API server [#3264](https://github.com/buildkite/agent/pull/3264) (@DrJosh9000)
- `AgentWorker` has `noWaitBetweenPingsForTesting` field [#3262](https://github.com/buildkite/agent/pull/3262) (@pda)
- refactor: rename `AgentRegisterResponse` local vars to `reg` consistently [#3259](https://github.com/buildkite/agent/pull/3259) (@pda)

### Dependencies
- Bump the container-images group across 6 directories with 2 updates [#3266](https://github.com/buildkite/agent/pull/3266) (@dependabot[bot])
- Bump the cloud-providers group across 1 directory with 3 updates [#3267](https://github.com/buildkite/agent/pull/3267) (@dependabot[bot])
- Bump the golang-x group with 4 updates [#3265](https://github.com/buildkite/agent/pull/3265) (@dependabot[bot])
- Bump golang.org/x/net from 0.37.0 to 0.38.0 in the golang-x group [#3256](https://github.com/buildkite/agent/pull/3256) (@dependabot[bot])
- Bump the container-images group across 4 directories with 1 update [#3258](https://github.com/buildkite/agent/pull/3258) (@dependabot[bot])
- Bump the cloud-providers group across 1 directory with 2 updates [#3252](https://github.com/buildkite/agent/pull/3252) (@dependabot[bot])
- Bump the container-images group across 5 directories with 2 updates [#3251](https://github.com/buildkite/agent/pull/3251) (@dependabot[bot])
- Bump gopkg.in/DataDog/dd-trace-go.v1 from 1.72.1 to 1.72.2 [#3250](https://github.com/buildkite/agent/pull/3250) (@dependabot[bot])

## [v3.95.1](https://github.com/buildkite/agent/tree/v3.95.1) (2025-03-20)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.95.0...v3.95.1)

> [!IMPORTANT]
> Secrets (as visible to the agent in environment variables) are now redacted from annotations, meta-data values, and step updates, similar to how secrets are redacted from job logs.
> If needed, this can be disabled by passing the flag `--redacted-vars=''` to the `annotate`, `meta-data set`, or `step update` command.

### Security
- Fix incomplete processing in newly-redacted operations [#3246](https://github.com/buildkite/agent/pull/3246) (@DrJosh9000)
- Bump github.com/golang-jwt/jwt/v5 from 5.2.1 to 5.2.2 (resolves CVE-2025-30204) [#3247](https://github.com/buildkite/agent/pull/3247) (@dependabot[bot])

## [v3.95.0](https://github.com/buildkite/agent/tree/v3.95.0) (2025-03-20)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.94.0...v3.95.0)

> [!IMPORTANT]
> Secrets (as visible to the agent in environment variables) are now redacted from annotations, meta-data values, and step updates, similar to how secrets are redacted from job logs.
> If needed, this can be disabled by passing the flag `--redacted-vars=''` to the `annotate`, `meta-data set`, or `step update` command.

### Changed
- Redact meta-data values and step attribute updates with warnings [#3243](https://github.com/buildkite/agent/pull/3243) (@DrJosh9000)
- Redact annotations [#3242](https://github.com/buildkite/agent/pull/3242) (@DrJosh9000)
- ANSI parser speedup [#3237](https://github.com/buildkite/agent/pull/3237) (@DrJosh9000)

### Fixed
- Agents running with disconnect-after-job or disconnect-after-idle-timeout can now be kept alive with agent pausing [#3238](https://github.com/buildkite/agent/pull/3238) (@DrJosh9000)
- The `pty-raw` experiment no longer causes a warning to be logged [#3241](https://github.com/buildkite/agent/pull/3241) (@DrJosh9000)

### Dependency updates
- Bump google.golang.org/api from 0.224.0 to 0.226.0 in the cloud-providers group [#3240](https://github.com/buildkite/agent/pull/3240) (@dependabot[bot])
- Bump the container-images group across 7 directories with 3 updates [#3239](https://github.com/buildkite/agent/pull/3239) (@dependabot[bot])

## [v3.94.0](https://github.com/buildkite/agent/tree/v3.94.0) (2025-03-12)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.93.1...v3.94.0)

### Security
- Fix redaction of multiline secrets printed as single lines [#3233](https://github.com/buildkite/agent/pull/3233) (@DrJosh9000)

### Changed
- Change healthHandler to log requests at debug level [#3232](https://github.com/buildkite/agent/pull/3232) (@DrJosh9000)
- go.mod: go 1.23.0, toolchain go1.23.7 [#3225](https://github.com/buildkite/agent/pull/3225) (@DrJosh9000)
- Record build URL in the buildkite-agent log for easier traceability [#3215](https://github.com/buildkite/agent/pull/3215) (@mkrapivner-zipline)

### Added
- Adding an initial bazel configuration [#3141](https://github.com/buildkite/agent/pull/3141) (@CerealBoy)

### Dependency bumps
- [#3228](https://github.com/buildkite/agent/pull/3228), [#3230](https://github.com/buildkite/agent/pull/3230), [#3229](https://github.com/buildkite/agent/pull/3229), [#3231](https://github.com/buildkite/agent/pull/3231), [#3222](https://github.com/buildkite/agent/pull/3222), [#3220](https://github.com/buildkite/agent/pull/3220), [#3221](https://github.com/buildkite/agent/pull/3221), [#3216](https://github.com/buildkite/agent/pull/3216), [#3217](https://github.com/buildkite/agent/pull/3217), [#3218](https://github.com/buildkite/agent/pull/3218) (@dependabot[bot])

## [v3.93.1](https://github.com/buildkite/agent/tree/v3.93.1) (2025-02-27)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.93.0...v3.93.1)

### Added
- Set env when job cancelled for hooks [#3213](https://github.com/buildkite/agent/pull/3213) (@sj26)

## [v3.93.0](https://github.com/buildkite/agent/tree/v3.93.0) (2025-02-26)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.92.1...v3.93.0)

### Added
- Handle pause actions [#3211](https://github.com/buildkite/agent/pull/3211) (@DrJosh9000)
- Add agent stop command [#3198](https://github.com/buildkite/agent/pull/3198) (@sj26)

### Changed
- Skip pushing the git commit metadata if BUILDKITE_COMMIT_RESOLVED=true [#3152](https://github.com/buildkite/agent/pull/3152) (@CerealBoy)
- Update cancel_signal.go [#3197](https://github.com/buildkite/agent/pull/3197) (@karensawrey)
- Capture datadog metrics usage from registering agents [#3195](https://github.com/buildkite/agent/pull/3195) (@wolfeidau)
- Capture some HTTP client details from registering agents [#3193](https://github.com/buildkite/agent/pull/3193) (@yob)

### Fixed
- Change the signal handler to ensure the agent quits after the grace period [#3200](https://github.com/buildkite/agent/pull/3200) (@wolfeidau)
- Don't fail if the interrupt fails when the PID is already exited [#3199](https://github.com/buildkite/agent/pull/3199) (@wolfeidau)
- bash shouldn't be assumed to be in /bin for portability [#1534](https://github.com/buildkite/agent/pull/1534) (@jgedarovich)

### Internal
- Fixes from the new modernize analyzer from the Go team [#3209](https://github.com/buildkite/agent/pull/3209) (@wolfeidau)
- Kill exp/maps and replace with stdlib maps [#3210](https://github.com/buildkite/agent/pull/3210) (@moskyb)

### Dependabot
- Dependencies - they just keep being updated! [#3203](https://github.com/buildkite/agent/pull/3203), [#3208](https://github.com/buildkite/agent/pull/3208), [#3205](https://github.com/buildkite/agent/pull/3205), [#3204](https://github.com/buildkite/agent/pull/3204), [#3207](https://github.com/buildkite/agent/pull/3207), [#3183](https://github.com/buildkite/agent/pull/3183), [#3186](https://github.com/buildkite/agent/pull/3186), [#3194](https://github.com/buildkite/agent/pull/3194) (@dependabot[bot])


## [v3.92.1](https://github.com/buildkite/agent/tree/v3.92.1) (2025-02-13)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.92.0...v3.92.1)

### Removed

- Revert "Ensure the log streamer respects forced shutdown of the agent" [#3191](https://github.com/buildkite/agent/pull/3191) (@wolfeidau)
- Revert "Fix data race on exitImmediately" [#3190](https://github.com/buildkite/agent/pull/3190) (@wolfeidau)

### Dependabot
- The usual updates: [#3188](https://github.com/buildkite/agent/pull/3188), [#3185](https://github.com/buildkite/agent/pull/3185) (@dependabot[bot])

> [!NOTE]
> Reverted [#3180](https://github.com/buildkite/agent/pull/3180) and [#3187](https://github.com/buildkite/agent/pull/3187) as this change introduced a bug which resulted in truncated log output. Will re-think this fix and push it out again in another release after we do some more testing.

## [v3.92.0](https://github.com/buildkite/agent/tree/v3.92.0) (2025-02-12)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.91.0...v3.92.0)

# Fixed
- Ensure the log streamer respects forced shutdown of the agent [#3180](https://github.com/buildkite/agent/pull/3180) (@wolfeidau)
- Fix data race on exitImmediately [#3187](https://github.com/buildkite/agent/pull/3187) (@DrJosh9000)
- Reduce timeout for these two operations to avoid holding up compute [#3177](https://github.com/buildkite/agent/pull/3177) (@wolfeidau)
- Timeout waiting for client containers [#3172](https://github.com/buildkite/agent/pull/3172) (@DrJosh9000)
- Clean up worker pool implementation [#3171](https://github.com/buildkite/agent/pull/3171) (@DrJosh9000)

### Internal
- rm bazel-*, add to .gitignore [#3178](https://github.com/buildkite/agent/pull/3178) (@DrJosh9000)
- Speed up needlessly slow tests [#3179](https://github.com/buildkite/agent/pull/3179) (@DrJosh9000)

### Dependabot
- The usual updates: [#3184](https://github.com/buildkite/agent/pull/3184), [#3182](https://github.com/buildkite/agent/pull/3182), [#3174](https://github.com/buildkite/agent/pull/3174), [#3173](https://github.com/buildkite/agent/pull/3173), [#3176](https://github.com/buildkite/agent/pull/3176) (@dependabot[bot])

## [v3.91.0](https://github.com/buildkite/agent/tree/v3.91.0) (2025-01-28)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.90.0...v3.91.0)

### Changed
- Jitter within ping, status, log loops [#3164](https://github.com/buildkite/agent/pull/3164) (@DrJosh9000)

### Fixed
- Roko v1.3.1 [#3157](https://github.com/buildkite/agent/pull/3157) (@moskyb)
- Better plugin checkout logging [#3166](https://github.com/buildkite/agent/pull/3166) (@DrJosh9000)

### Internal
- Add /.buildkite dir for Dockerfile updates [#3162](https://github.com/buildkite/agent/pull/3162) (@DrJosh9000)

<details>
<summary><h3>Dependency bumps</h3></summary>

- Bump the cloud-providers group with 6 updates [#3167](https://github.com/buildkite/agent/pull/3167) (@dependabot[bot])
- Bump gopkg.in/DataDog/dd-trace-go.v1 from 1.70.3 to 1.71.0 [#3168](https://github.com/buildkite/agent/pull/3168) (@dependabot[bot])
- Bump the container-images group across 5 directories with 2 updates [#3169](https://github.com/buildkite/agent/pull/3169) (@dependabot[bot])
- Bump the otel group with 9 updates [#3159](https://github.com/buildkite/agent/pull/3159) (@dependabot[bot])
- Bump the container-images group across 6 directories with 2 updates [#3161](https://github.com/buildkite/agent/pull/3161) (@dependabot[bot])
- Bump the cloud-providers group across 1 directory with 7 updates [#3160](https://github.com/buildkite/agent/pull/3160) (@dependabot[bot])
- Bump gopkg.in/DataDog/dd-trace-go.v1 from 1.70.1 to 1.70.3 [#3155](https://github.com/buildkite/agent/pull/3155) (@dependabot[bot])
- Bump the golang-x group across 1 directory with 5 updates [#3151](https://github.com/buildkite/agent/pull/3151) (@dependabot[bot])
- Bump buildkite/agent-base from `e46604b` to `2520343` in /packaging/docker/ubuntu-22.04 in the container-images group across 1 directory [#3146](https://github.com/buildkite/agent/pull/3146) (@dependabot[bot])

</details>

## [v3.90.0](https://github.com/buildkite/agent/tree/v3.90.0) (2025-01-10)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.89.0...v3.90.0)

### Changed
- Use exponential in acquire-job mode when job acquisition fails [#3153](https://github.com/buildkite/agent/pull/3153) (@moskyb)

### Fixed
- Fix nil pointer deref in certain Kubernetes environments [#3150](https://github.com/buildkite/agent/pull/3150) (@DrJosh9000)

## [v3.89.0](https://github.com/buildkite/agent/tree/v3.89.0) (2025-01-06)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.88.0...v3.89.0)

### Added
- Adding support for Additional Hooks Paths [#3124](https://github.com/buildkite/agent/pull/3124) (@CerealBoy)

### Internal
- Bump the container-images group across 5 directories with 2 updates [#3143](https://github.com/buildkite/agent/pull/3143) (@dependabot[bot])
- Update golang.org/x/net [#3140](https://github.com/buildkite/agent/pull/3140) (@yob)

## [v3.88.0](https://github.com/buildkite/agent/tree/v3.88.0) (2024-12-18)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.87.1...v3.88.0)

### Changed
- Prefix fatal error message with 'buildkite-agent:' [#3135](https://github.com/buildkite/agent/pull/3135) (@jordandcarter)
- Notify when host and bootstrap agent paths mismatch [#3123](https://github.com/buildkite/agent/pull/3123) (@jordandcarter)

### Fixed
- Enable process debug logging [#3134](https://github.com/buildkite/agent/pull/3134) (@patrobinson)
- Ignore empty submodule clone configs [#3122](https://github.com/buildkite/agent/pull/3122) (@DrJosh9000)
- fix: allow for empty files on hook check [#3117](https://github.com/buildkite/agent/pull/3117) (@nzspambot)
- Parse more standalone `$` cases as literal `$`s and not variable expansions:
  - Bump github.com/buildkite/go-pipeline from 0.13.2 to 0.13.3 [#3137](https://github.com/buildkite/agent/pull/3137) (@dependabot[bot])
  - Bump github.com/buildkite/interpolate from 0.1.4 to 0.1.5 [#3138](https://github.com/buildkite/agent/pull/3138) (@dependabot[bot])

### Dependabot
- [#3136](https://github.com/buildkite/agent/pull/3136), [#3127](https://github.com/buildkite/agent/pull/3127), [#3129](https://github.com/buildkite/agent/pull/3129), [#3128](https://github.com/buildkite/agent/pull/3128), [#3130](https://github.com/buildkite/agent/pull/3130), [#3132](https://github.com/buildkite/agent/pull/3132), [#3131](https://github.com/buildkite/agent/pull/3131), [#3133](https://github.com/buildkite/agent/pull/3133), [#3125](https://github.com/buildkite/agent/pull/3125), [#3119](https://github.com/buildkite/agent/pull/3119), [#3120](https://github.com/buildkite/agent/pull/3120), [#3121](https://github.com/buildkite/agent/pull/3121), [#3116](https://github.com/buildkite/agent/pull/3116), [#3115](https://github.com/buildkite/agent/pull/3115) (@dependabot[bot])

## [v3.87.1](https://github.com/buildkite/agent/tree/v3.87.1) (2024-11-26)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.87.0...v3.87.1)

### Fixed
- Fix duplicated output when debug is enabled [#3108](https://github.com/buildkite/agent/pull/3108) (@DrJosh9000)

### Changed
- Small change to annotation example [#3106](https://github.com/buildkite/agent/pull/3106) (@PriyaSudip)

### Internal
- Use Ubuntu codename labels to refer to base images [#3103](https://github.com/buildkite/agent/pull/3103) (@DrJosh9000)

### Dependabot
- The usual updates: [#3111](https://github.com/buildkite/agent/pull/3111), [#3112](https://github.com/buildkite/agent/pull/3112), [#3110](https://github.com/buildkite/agent/pull/3110), [#3109](https://github.com/buildkite/agent/pull/3109), [#3113](https://github.com/buildkite/agent/pull/3113), [#3104](https://github.com/buildkite/agent/pull/3104), [#3098](https://github.com/buildkite/agent/pull/3098), [#3102](https://github.com/buildkite/agent/pull/3102), [#3097](https://github.com/buildkite/agent/pull/3097), [#3101](https://github.com/buildkite/agent/pull/3101) (@dependabot[bot])

## [v3.87.0](https://github.com/buildkite/agent/tree/v3.87.0) (2024-11-18)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.86.0...v3.87.0)

### Changed
- Remove signal reason unable\_to\_verify\_signature and replace with signature\_rejected [#3094](https://github.com/buildkite/agent/pull/3094) (@jordandcarter)

### Fixed
- Don't surface expected stderr output from git rev-parse [#3095](https://github.com/buildkite/agent/pull/3095) (@CerealBoy)
- Add retry around NewS3Client [#3092](https://github.com/buildkite/agent/pull/3092) (@l-suzuki)

### Internal
- Soft fail upload of packages docker images [#3093](https://github.com/buildkite/agent/pull/3093) (@tommeier)
- Switch to agent-base images [#3091](https://github.com/buildkite/agent/pull/3091) (@DrJosh9000)

## [v3.86.0](https://github.com/buildkite/agent/tree/v3.86.0) (2024-11-12)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.85.1...v3.86.0)

### Added
- Add `force-grace-period-seconds` argument to `step cancel` command [#3084](https://github.com/buildkite/agent/pull/3084) (@mitchbne)

### Changed
- Rename env var to `BUILDKITE_STEP_CANCEL_FORCE_GRACE_PERIOD_SECONDS` [#3087](https://github.com/buildkite/agent/pull/3087) (@mitchbne)
- Drop Ubuntu 18.04, add Ubuntu 24.04 [#3078](https://github.com/buildkite/agent/pull/3078) (@DrJosh9000)

### Fixed
- Handle older version of remote ref error message [#3082](https://github.com/buildkite/agent/pull/3082) (@steveh)

### Internal
- dependabot: Group Dockerfiles [#3077](https://github.com/buildkite/agent/pull/3077) (@DrJosh9000)
- Various dependency bumps: [#3086](https://github.com/buildkite/agent/pull/3086), [#3085](https://github.com/buildkite/agent/pull/3085), [#3081](https://github.com/buildkite/agent/pull/3081), [#3079](https://github.com/buildkite/agent/pull/3079) (@dependabot[bot])

## [v3.85.1](https://github.com/buildkite/agent/tree/v3.85.1) (2024-11-09)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.85.0...v3.85.1)

### Fixed
- Fix another nil pointer panic in k8s mode [#3075](https://github.com/buildkite/agent/pull/3075) (@DrJosh9000)
- Fix nil pointer panic in k8s mode [#3074](https://github.com/buildkite/agent/pull/3074) (@DrJosh9000)

## [v3.85.0](https://github.com/buildkite/agent/tree/v3.85.0) (2024-11-07)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.84.0...v3.85.0)

### Added
- Create `buildkite-agent step cancel` subcommand [#3070](https://github.com/buildkite/agent/pull/3070) (@mitchbne)

### Changed
- Support installing specific versions via script [#3069](https://github.com/buildkite/agent/pull/3069) (@jordandcarter)
- Promote polyglot-hooks experiment to default [#3063](https://github.com/buildkite/agent/pull/3063) (@DrJosh9000)
- Use sha256 in the checksum verification [#3062](https://github.com/buildkite/agent/pull/3062) (@esenmarti)
- Minor update to the 'redactor' CLI command examples. [#3060](https://github.com/buildkite/agent/pull/3060) (@gilesgas)

### Fixed
- Fix zzglob import path [#3057](https://github.com/buildkite/agent/pull/3057) (@DrJosh9000)

### Internal
- Shell package cleanup [#3068](https://github.com/buildkite/agent/pull/3068) (@DrJosh9000)
- Remove .editorconfig [#3064](https://github.com/buildkite/agent/pull/3064) (@DrJosh9000)
- Various dependency bumps: [#3066](https://github.com/buildkite/agent/pull/3066) [#3065](https://github.com/buildkite/agent/pull/3065) [#3067](https://github.com/buildkite/agent/pull/3067) (@dependabot[bot])

## [v3.84.0](https://github.com/buildkite/agent/tree/v3.84.0) (2024-10-28)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.83.1...v3.84.0)

### Added
- Add command for canceling a running a build [#2958](https://github.com/buildkite/agent/pull/2958) (@dannymidnight)
- Add tini-static to alpine images [#3054](https://github.com/buildkite/agent/pull/3054) (@DrJosh9000)

### Fixed
- Implement several documentation improvements to the Agent (for the Buildkite Docs). [#3043](https://github.com/buildkite/agent/pull/3043) (@gilesgas)
- Allow token to be empty if graphql-token is provided [#3051](https://github.com/buildkite/agent/pull/3051) (@jordandcarter)
- Fix multiline secret redaction when output with \r\n [#3050](https://github.com/buildkite/agent/pull/3050) (@DrJosh9000)
- k8s exec: Perform liveness check of clients [#3045](https://github.com/buildkite/agent/pull/3045) (@DrJosh9000)
- Fix request headers for multipart [#3042](https://github.com/buildkite/agent/pull/3042) (@DrJosh9000)

### Internal
- install.sh tidyups [#3032](https://github.com/buildkite/agent/pull/3032) (@DrJosh9000)
- Parallel container image uploads [#3035](https://github.com/buildkite/agent/pull/3035) (@DrJosh9000)
- Various dependency bumps: [#3058](https://github.com/buildkite/agent/pull/3058), [#3026](https://github.com/buildkite/agent/pull/3026), [#3055](https://github.com/buildkite/agent/pull/3055), [#3056](https://github.com/buildkite/agent/pull/3056), [#3048](https://github.com/buildkite/agent/pull/3048), [#3047](https://github.com/buildkite/agent/pull/3047), [#3049](https://github.com/buildkite/agent/pull/3049), [#3036](https://github.com/buildkite/agent/pull/3036), [#3041](https://github.com/buildkite/agent/pull/3041), [#3040](https://github.com/buildkite/agent/pull/3040), [#3037](https://github.com/buildkite/agent/pull/3037), [#3039](https://github.com/buildkite/agent/pull/3039) (@dependabot[bot])

## [v3.83.1](https://github.com/buildkite/agent/tree/v3.83.0) (2024-10-10)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.83.0...v3.83.1)

### Fixed
- Fix artifact up/download timeouts [#3033](https://github.com/buildkite/agent/pull/3033) (@DrJosh9000)

## [v3.83.0](https://github.com/buildkite/agent/tree/v3.83.0) (2024-10-08)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.82.1...v3.83.0)

### Added
- Exit with code 94 if a mirror lock times out [#3023](https://github.com/buildkite/agent/pull/3023) (@DrJosh9000)
- Add support for oidc aws session tags [#3015](https://github.com/buildkite/agent/pull/3015) (@sj26)
- Support for future multipart artifact uploads [#2991](https://github.com/buildkite/agent/pull/2991) (@DrJosh9000)

### Fixed
- Tweak BUILDKITE_IGNORED_ENV handling [#3029](https://github.com/buildkite/agent/pull/3029) (@DrJosh9000)
- BUG FIX: Ensure Build Title Is Correct When Checkout Is Skipped [#3024](https://github.com/buildkite/agent/pull/3024) (@123sarahj123)
- Ensure all string slice args have whitespace cleaned off of each element [#3021](https://github.com/buildkite/agent/pull/3021) (@moskyb)
- Fix data race on worker stop [#3016](https://github.com/buildkite/agent/pull/3016) (@DrJosh9000)

### Internal
- Migrate Agent Pipeline to Agent Cluster [#3018](https://github.com/buildkite/agent/pull/3018) (@matthewborden)
- Refactor the various agent HTTP clients [#3017](https://github.com/buildkite/agent/pull/3017) (@DrJosh9000)
- Dependabot bumps to busybox [#3025](https://github.com/buildkite/agent/pull/3025), golang.org/x packages [#3027](https://github.com/buildkite/agent/pull/3027), cloud provider packages [#3028](https://github.com/buildkite/agent/pull/3028), [#3019](https://github.com/buildkite/agent/pull/3019), [#3013](https://github.com/buildkite/agent/pull/3013), [#3009](https://github.com/buildkite/agent/pull/3009), DataDog packages [#3010](https://github.com/buildkite/agent/pull/3010) Ubuntu [#3012](https://github.com/buildkite/agent/pull/3012), [#3008](https://github.com/buildkite/agent/pull/3008), and go-pipeline [#3014](https://github.com/buildkite/agent/pull/3014) (@dependabot[bot])

## [v3.82.1](https://github.com/buildkite/agent/tree/v3.82.1) (2024-09-23)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.82.0...v3.82.1)

### Fixed
- Work around issue with http2 connections on linux not cleanly closing, causing agents to be marked as lost [#3005](https://github.com/buildkite/agent/pull/3005) (@patrobinson)

## [v3.82.0](https://github.com/buildkite/agent/tree/v3.82.0) (2024-09-17)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.81.0...v3.82.0)

### Added
- Emit HTTP timings [#2989](https://github.com/buildkite/agent/pull/2989) (@patrobinson)
- Add JSON-format env file, allow annotations from pre-bootstrap [#2988](https://github.com/buildkite/agent/pull/2988) (@DrJosh9000)

### Changed
- Remove mitchellh/go-homedir; it's archived [#2990](https://github.com/buildkite/agent/pull/2990) (@mckern)

### Fixed
- Use job tokens for log chunk uploads [#2986](https://github.com/buildkite/agent/pull/2986) (@tessereth)
- Temporarily pin kubectl version [#2997](https://github.com/buildkite/agent/pull/2997) (@patrobinson)
- Prefer $HOME on all platforms [#3000](https://github.com/buildkite/agent/pull/3000) (@DrJosh9000)
- Bump github.com/buildkite/interpolate from 0.1.3 to 0.1.4 [#3002](https://github.com/buildkite/agent/pull/3002)  (Fixes a bug in nested variable interpolation https://github.com/buildkite/interpolate/pull/15)

### Internal
- Dependabot churn: [#2992](https://github.com/buildkite/agent/pull/2992) [#2993](https://github.com/buildkite/agent/pull/2993) [#2995](https://github.com/buildkite/agent/pull/2995) [#2996](https://github.com/buildkite/agent/pull/2996) [#2979](https://github.com/buildkite/agent/pull/2979) [#2981](https://github.com/buildkite/agent/pull/2981)
- Consolidate artifact functionality in internal package [#2985](https://github.com/buildkite/agent/pull/2985) (@DrJosh9000)


## [v3.81.0](https://github.com/buildkite/agent/tree/v3.81.0) (2024-09-10)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.80.0...v3.81.0)

### Fixed
- Fix for region discovery issue with aws sdkv2 when running in ec2 [#2977](https://github.com/buildkite/agent/pull/2977) (@wolfeidau)
- Explain verification-failure-behavior in more detail [#2984](https://github.com/buildkite/agent/pull/2984) (@DrJosh9000)

### Added
- Add sha256 checksum output to the formatting options [#2974](https://github.com/buildkite/agent/pull/2974) (@patrobinson)

### Internal
- Dependabot churn: [#2978](https://github.com/buildkite/agent/pull/2978), [#2980](https://github.com/buildkite/agent/pull/2980)

## [v3.80.0](https://github.com/buildkite/agent/tree/v3.80.0) (2024-09-06)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.79.0...v3.80.0)

### Added
- Support AWS KMS for signing and verifying pipelines [#2960](https://github.com/buildkite/agent/pull/2960) (@wolfeidau)

### Changed
- Allow `buildkite-agent` to run a job when JWK is unavailable but failure behaviour is set to `warn` [#2945](https://github.com/buildkite/agent/pull/2945) (@CheeseStick)

### Fixed
- coda-content-type pass content-type to the server when specified [#2967](https://github.com/buildkite/agent/pull/2967) (@SorchaAbel)
- Updated to support only ECC_NIST_P256 keyspec for initial release [#2973](https://github.com/buildkite/agent/pull/2973) (@wolfeidau)

### Internal
- Dependabot churn: [#2964](https://github.com/buildkite/agent/pull/2964), [#2965](https://github.com/buildkite/agent/pull/2965), [#2952](https://github.com/buildkite/agent/pull/2952), [#2972](https://github.com/buildkite/agent/pull/2972), [#2963](https://github.com/buildkite/agent/pull/2963)

## [v3.79.0](https://github.com/buildkite/agent/tree/v3.79.0) (2024-08-29)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.78.0...v3.79.0)

### Fixed
- Fix error when hook cannot be run due to missing interpreter [#2948](https://github.com/buildkite/agent/pull/2948) (@mcncl)

### Added
- Support for multiple trace context encodings [#2947](https://github.com/buildkite/agent/pull/2947) (@DrJosh9000)

### Internal
- Bump github.com/buildkite/go-pipeline from 0.11.0 to 0.12.0 [#2959](https://github.com/buildkite/agent/pull/2959) (@wolfeidau)
- Dependabot churn: [#2951](https://github.com/buildkite/agent/pull/2951), [#2955](https://github.com/buildkite/agent/pull/2955), [#2949](https://github.com/buildkite/agent/pull/2949), [#2956](https://github.com/buildkite/agent/pull/2956), [#2954](https://github.com/buildkite/agent/pull/2954), [#2950](https://github.com/buildkite/agent/pull/2950), [#2953](https://github.com/buildkite/agent/pull/2953)

## [v3.78.0](https://github.com/buildkite/agent/tree/v3.78.0) (2024-08-20)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.77.0...v3.78.0)

### Fixed
- fix for layout issues with log messages [#2933](https://github.com/buildkite/agent/pull/2933) (@wolfeidau)
- Prevent Cancel from running when a k8s job is cancelled already [#2935](https://github.com/buildkite/agent/pull/2935) (@CerealBoy)
- k8s: Unconditionally set `BUILDKITE_AGENT_ACCESS_TOKEN` [#2942](https://github.com/buildkite/agent/pull/2942) (@DrJosh9000)

### Changed
- Add a bit more context to the debugging for failing signature verify [#2926](https://github.com/buildkite/agent/pull/2926) (@wolfeidau)
- Update description for the 'priority' option for the 'buildkite-agent annotate' command. [#2934](https://github.com/buildkite/agent/pull/2934) (@gilesgas)

### Internal
Dependabot churn: [#2927](https://github.com/buildkite/agent/pull/2927), [#2928](https://github.com/buildkite/agent/pull/2928), [#2929](https://github.com/buildkite/agent/pull/2929), [#2930](https://github.com/buildkite/agent/pull/2930), [#2931](https://github.com/buildkite/agent/pull/2931), [#2937](https://github.com/buildkite/agent/pull/2937), [#2939](https://github.com/buildkite/agent/pull/2939), [#2940](https://github.com/buildkite/agent/pull/2940), [#2943](https://github.com/buildkite/agent/pull/2943)

## [v3.77.0](https://github.com/buildkite/agent/tree/v3.77.0) (2024-08-08)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.76.2...v3.77.0)

### Added
- Added `core` package: `core` makes some core agent functions accessible as a library [#2915](https://github.com/buildkite/agent/pull/2915) (@DrJosh9000)

### Fixed
- Write hooks into new tempdir [#2925](https://github.com/buildkite/agent/pull/2925) (@DrJosh9000)
- Fix default endpoint string in `api` and `core` [#2923](https://github.com/buildkite/agent/pull/2923) (@DrJosh9000)

### Internal
Dependabot churn: [#2919](https://github.com/buildkite/agent/pull/2919), [#2922](https://github.com/buildkite/agent/pull/2922), [#2921](https://github.com/buildkite/agent/pull/2921), [#2918](https://github.com/buildkite/agent/pull/2918), [#2917](https://github.com/buildkite/agent/pull/2917)

## [v3.76.2](https://github.com/buildkite/agent/tree/v3.76.2) (2024-08-01)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.76.1...v3.76.2)

> [!NOTE]
> v3.76.0 fixed an issue which caused the HTTP client in the agent to fall back to HTTP/1.1, see [#2908](https://github.com/buildkite/agent/pull/2908). If you need to disable HTTP/2.0 in your environment you can do this using the `--no-http2` flag or matching configuration option.

### Fixed
- Only override TLSClientConfig if set [#2913](https://github.com/buildkite/agent/pull/2913) (@DrJosh9000)

## [v3.76.1](https://github.com/buildkite/agent/tree/v3.76.1) (2024-07-31)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.76.0...v3.76.1)

> [!NOTE]
> v3.76.0 fixed an issue which caused the HTTP client in the agent to fall back to HTTP/1.1, see [#2908](https://github.com/buildkite/agent/pull/2908). If you need to disable HTTP/2.0 in your environment you can do this using the `--no-http2` flag or matching configuration option.

### Changed
- Pass cancel grace period to bootstrap [#2910](https://github.com/buildkite/agent/pull/2910) (@DrJosh9000)

## [v3.76.0](https://github.com/buildkite/agent/tree/v3.76.0) (2024-07-31)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.75.1...v3.76.0)

> [!NOTE]
> This release fixed an issue which caused the HTTP client in the agent to fall back to HTTP/1.1, see [#2908](https://github.com/buildkite/agent/pull/2908). If you need to disable HTTP/2.0 in your environment you can do this using the `--no-http2` flag or matching configuration option.

### Changed
- fix enable http/2 by default as intended by flags [#2908](https://github.com/buildkite/agent/pull/2908) (@wolfeidau)

### Fixed
- Let artifact phase and post-command run in grace period [#2899](https://github.com/buildkite/agent/pull/2899) (@DrJosh9000)

### Internal
- Dependabot updates: [#2902](https://github.com/buildkite/agent/pull/2902), [#2907](https://github.com/buildkite/agent/pull/2907), [#2903](https://github.com/buildkite/agent/pull/2903), [#2904](https://github.com/buildkite/agent/pull/2904), [#2901](https://github.com/buildkite/agent/pull/2901), [#2905](https://github.com/buildkite/agent/pull/2905), [#2896](https://github.com/buildkite/agent/pull/2896), [#2897](https://github.com/buildkite/agent/pull/2897)

## [v3.75.1](https://github.com/buildkite/agent/tree/v3.75.1) (2024-07-22)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.75.0...v3.75.1)

### Fixed
- Fix downloaded artifact permissions [#2894](https://github.com/buildkite/agent/pull/2894) (@DrJosh9000)

## [v3.75.0](https://github.com/buildkite/agent/tree/v3.75.0) (2024-07-18)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.74.1...v3.75.0)

### Added
- Introduce `riscv64` architecture [#2877](https://github.com/buildkite/agent/pull/2877) (@TimePrinciple)
- Add a SHA256SUMS file [#2890](https://github.com/buildkite/agent/pull/2890) (@DrJosh9000)

### Changed
- Reject more secrets [#2884](https://github.com/buildkite/agent/pull/2884) (@DrJosh9000)
- Include repo name in Packages image path [#2871](https://github.com/buildkite/agent/pull/2871) (@swebb)

### Fixed
- Fix some common artifact download bugs [#2878](https://github.com/buildkite/agent/pull/2878) (@DrJosh9000)
- SUP-2343: remove "retry" example from "buildkite-agent step get" as not valid [#2879](https://github.com/buildkite/agent/pull/2879) (@tomowatt)

### Internal
- Log in to buildkite packages right before pushing images [#2892](https://github.com/buildkite/agent/pull/2892) (@moskyb)
- Update LICENSE.txt [#2885](https://github.com/buildkite/agent/pull/2885) (@wooly)
- Remove Packagecloud agent publish steps from agent pipeline [#2873](https://github.com/buildkite/agent/pull/2873) (@tommeier)
- Release Docker images on Buildkite Packages [#2837](https://github.com/buildkite/agent/pull/2837) (@swebb)
- Fix the OIDC login for Packages [#2875](https://github.com/buildkite/agent/pull/2875) (@swebb)
- Fix the Packages registry name [#2874](https://github.com/buildkite/agent/pull/2874) (@swebb)
- Fix image name when pushing to Buildkite packages [#2870](https://github.com/buildkite/agent/pull/2870) (@swebb)
- Dependabot updates: [#2888](https://github.com/buildkite/agent/pull/2888), [#2887](https://github.com/buildkite/agent/pull/2887), [#2882](https://github.com/buildkite/agent/pull/2882), [#2883](https://github.com/buildkite/agent/pull/2883), [#2880](https://github.com/buildkite/agent/pull/2880)

## [v3.74.1](https://github.com/buildkite/agent/tree/v3.74.1) (2024-07-03)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.74.0...v3.74.1)

### Added
- Log public signing key thumbprint and signed step payload [#2853](https://github.com/buildkite/agent/pull/2853) (@jordandcarter)

### Fixed
- Don't try to early-set env vars [#2852](https://github.com/buildkite/agent/pull/2852) (@DrJosh9000)
- Convey env vars between k8s containers [#2851](https://github.com/buildkite/agent/pull/2851) (@DrJosh9000)
- Fix typo in "kuberentes" [#2836](https://github.com/buildkite/agent/pull/2836) (@moskyb)

### Internal
- Make the graphql endpoint for `buildkite-agent tool sign` configurable [#2841](https://github.com/buildkite/agent/pull/2841) (@moskyb)
- Dependabot updates: [#2863](https://github.com/buildkite/agent/pull/2863), [#2862](https://github.com/buildkite/agent/pull/2862), [#2857](https://github.com/buildkite/agent/pull/2857), [#2860](https://github.com/buildkite/agent/pull/2860), [#2864](https://github.com/buildkite/agent/pull/2864), [#2856](https://github.com/buildkite/agent/pull/2856), [#2867](https://github.com/buildkite/agent/pull/2867), [#2846](https://github.com/buildkite/agent/pull/2846), [#2848](https://github.com/buildkite/agent/pull/2848), [#2847](https://github.com/buildkite/agent/pull/2847), [#2845](https://github.com/buildkite/agent/pull/2845), [#2840](https://github.com/buildkite/agent/pull/2840), [#2844](https://github.com/buildkite/agent/pull/2844), [#2842](https://github.com/buildkite/agent/pull/2842), [#2843](https://github.com/buildkite/agent/pull/2843), [#2849](https://github.com/buildkite/agent/pull/2849)

## [v3.74.0](https://github.com/buildkite/agent/tree/v3.74.0) (2024-06-11)
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
- Dependabot updates: [#2809](https://github.com/buildkite/agent/pull/2809), [#2816](https://github.com/buildkite/agent/pull/2816), [#2800](https://github.com/buildkite/agent/pull/2800), [#2801](https://github.com/buildkite/agent/pull/2801), [#2802](https://github.com/buildkite/agent/pull/2802), [#2803](https://github.com/buildkite/agent/pull/2803), [#2787](https://github.com/buildkite/agent/pull/2787), [#2798](https://github.com/buildkite/agent/pull/2798), [#2808](https://github.com/buildkite/agent/pull/2808), [#2827](https://github.com/buildkite/agent/pull/2827) [#2817](https://github.com/buildkite/agent/pull/2817), [#2818](https://github.com/buildkite/agent/pull/2818), [#2819](https://github.com/buildkite/agent/pull/2819), [#2822](https://github.com/buildkite/agent/pull/2822), [#2829](https://github.com/buildkite/agent/pull/2829), [#2832](https://github.com/buildkite/agent/pull/2832), [#2835](https://github.com/buildkite/agent/pull/2835)

## [v3.73.1](https://github.com/buildkite/agent/tree/v3.73.1) (2024-05-23)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.73.0...v3.73.1)

### Fixed

- Fix redaction when no initial redactors are present [#2794](https://github.com/buildkite/agent/pull/2794) (@moskyb)
- Fix an issue where intermittently, commands run by the agent would fail with `error: fork/exec: operation not permitted` [#2791](https://github.com/buildkite/agent/pull/2791) (@moskyb)
- Fix an issue where using cancel grace period would not work if signal grace period was not set [#2788](https://github.com/buildkite/agent/pull/2788) (@tessereth)
- Emit a better error if the job API token is missing [#2789](https://github.com/buildkite/agent/pull/2789) (@moskyb)

### Internal
- Bump docker/library/golang from `b1e05e2` to `f43c6f0` in /.buildkite [#2785](https://github.com/buildkite/agent/pull/2785)
- Upgrade math/rand to v2 [#2792](https://github.com/buildkite/agent/pull/2792) (@DrJosh9000)

## [v3.73.0](https://github.com/buildkite/agent/tree/v3.73.0) (2024-05-16)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.72.0...v3.73.0)

### Changed
- Return earlier from non-get credential actions [#2776](https://github.com/buildkite/agent/pull/2776) (@moskyb)
- Remove the --debug-http flag from the git credential helper [#2772](https://github.com/buildkite/agent/pull/2772) (@moskyb)
- Write "unknown exit status" in expanded section [#2783](https://github.com/buildkite/agent/pull/2783) (@DrJosh9000)

### Fixed
- Fix poorly-timed timestamp insertions [#2778](https://github.com/buildkite/agent/pull/2778) (@DrJosh9000)
- Fix typo in 'buildkite-agent redactor add' description. [#2777](https://github.com/buildkite/agent/pull/2777) (@gilesgas)
- Fix checkout race condition on GitHub PR builds [#2735](https://github.com/buildkite/agent/pull/2735) (@rianmcguire)
- Expand buildkite-agent secret command with a more useful description. [#2775](https://github.com/buildkite/agent/pull/2775) (@gilesgas)

### Internal
- Dependabot updates: [#2779](https://github.com/buildkite/agent/pull/2779), [#2782](https://github.com/buildkite/agent/pull/2782), [#2781](https://github.com/buildkite/agent/pull/2781), [#2771](https://github.com/buildkite/agent/pull/2771), [#2770](https://github.com/buildkite/agent/pull/2770), [#2769](https://github.com/buildkite/agent/pull/2769), [#2767](https://github.com/buildkite/agent/pull/2767)

## [v3.72.0](https://github.com/buildkite/agent/tree/v3.72.0) (2024-05-06)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.71.0...v3.72.0)

### Added
- Add status.json endpoint to health check endpoints [#2759](https://github.com/buildkite/agent/pull/2759) (@moskyb)

### Changed
- Make failed job acquisitions return a specific exit code (27) [#2762](https://github.com/buildkite/agent/pull/2762) (@moskyb)

### Internal
- Refactor agent integration test API [#2764](https://github.com/buildkite/agent/pull/2764) (@moskyb)
- Replace calls to %v for error values in fmt.Errorf with %w [#2763](https://github.com/buildkite/agent/pull/2763) (@moskyb)
- Release pipeline changes:
  - Pass AWS creds into docker containers [#2761](https://github.com/buildkite/agent/pull/2761) (@amu-g)
  - release: Pass AWS credentials to Docker containers [#2760](https://github.com/buildkite/agent/pull/2760) (@lucaswilric)
  - Use oidc roles in release pipelines [#2755](https://github.com/buildkite/agent/pull/2755) (@amu-g)
- Dependency updates [#2752](https://github.com/buildkite/agent/pull/2752), [#2751](https://github.com/buildkite/agent/pull/2751), [#2750](https://github.com/buildkite/agent/pull/2750), [#2739](https://github.com/buildkite/agent/pull/2739), [#2740](https://github.com/buildkite/agent/pull/2740), [#2753](https://github.com/buildkite/agent/pull/2753), [#2757](https://github.com/buildkite/agent/pull/2757)

## [v3.71.0](https://github.com/buildkite/agent/tree/v3.71.0) (2024-04-30)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.70.0...v3.71.0)

## Fixed
- Make preferring runtime env default off [#2747](https://github.com/buildkite/agent/pull/2747) (@patrobinson)
- Use roko to retry k8s socket dial [#2746](https://github.com/buildkite/agent/pull/2746) (@DrJosh9000)
- Tweak ETXTBSY retry, and be helpful for ENOENT [#2736](https://github.com/buildkite/agent/pull/2736) (@DrJosh9000)

### Added
- Experiment: override zero exit code on cancel [#2741](https://github.com/buildkite/agent/pull/2741) (@DrJosh9000)

## [v3.70.0](https://github.com/buildkite/agent/tree/v3.70.0) (2024-04-18)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.69.0...v3.70.0)

### Added
- Add BUILDKITE_STEP_KEY as a json logger field [#2730](https://github.com/buildkite/agent/pull/2730) (@joeljeske)
- New flag `--spawn-per-cpu` The number of agents to spawn per cpu in parallel (mutually exclusive with --spawn) [#2711](https://github.com/buildkite/agent/pull/2711) (@mmlb)
- Upload agent images to GHCR [#2724](https://github.com/buildkite/agent/pull/2724) (@DrJosh9000)

### Fixed
- Update go-pipeline to v0.7.0 (Correctly upload cache `name` and `size` command step settings, support `cache: false`) [#2731](https://github.com/buildkite/agent/pull/2731) (@jordandcarter)
- Show descriptive error when annotation body size exceeds maximum when using stdin [#2725](https://github.com/buildkite/agent/pull/2725) (@rianmcguire)

### Internal
- Dependabot updates [#2726](https://github.com/buildkite/agent/pull/2726) [#2727](https://github.com/buildkite/agent/pull/2727) [#2728](https://github.com/buildkite/agent/pull/2728) [#2729](https://github.com/buildkite/agent/pull/2729)

## [v3.69.0](https://github.com/buildkite/agent/tree/v3.69.0) (2024-04-10)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.68.0...v3.69.0)

### Added
- Environment variable to control cache volume mounting on hosted agents [#2720](https://github.com/buildkite/agent/pull/2720) [#2722](https://github.com/buildkite/agent/pull/2722) (@moskyb)

### Internal

- @dependabot, hard at work as usual [#2717](https://github.com/buildkite/agent/pull/2717) [#2721](https://github.com/buildkite/agent/pull/2721) [#2719](https://github.com/buildkite/agent/pull/2719) [#2718](https://github.com/buildkite/agent/pull/2718) [#2715](https://github.com/buildkite/agent/pull/2715) (@dependabot)

## [v3.68.0](https://github.com/buildkite/agent/tree/v3.68.0) (2024-04-04)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.67.0...v3.68.0)

### Changed
- Ensure that disabled warnings get passed to the logger in kubernetes agents [#2698](https://github.com/buildkite/agent/pull/2698) (@moskyb)
- Handle warnings from go-pipeline `Parse` [#2675](https://github.com/buildkite/agent/pull/2675) (@DrJosh9000)
- Don't run pre-exit hooks without command phase [#2707](https://github.com/buildkite/agent/pull/2707) (@DrJosh9000)

### Internal
- Dependabot updates [#2714](https://github.com/buildkite/agent/pull/2714), [#2712](https://github.com/buildkite/agent/pull/2712), [#2709](https://github.com/buildkite/agent/pull/2709), [#2708](https://github.com/buildkite/agent/pull/2708), [#2663](https://github.com/buildkite/agent/pull/2663)

## [v3.67.0](https://github.com/buildkite/agent/tree/v3.67.0) (2024-03-28)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.66.0...v3.67.0)

### Changed
- De-experiment isolated plugin checkout [#2694](https://github.com/buildkite/agent/pull/2694) (@triarius)
- Always set git commit [#2676](https://github.com/buildkite/agent/pull/2676) (@moskyb)
- Silence Job API Log Group [#2690](https://github.com/buildkite/agent/pull/2690), [#2695](https://github.com/buildkite/agent/pull/2695) (@triarius)
- Set a user agent when downloading most artifacts [#2671](https://github.com/buildkite/agent/pull/2671) (@yob)
- Extend default signal grace period to 9 seconds [#2696](https://github.com/buildkite/agent/pull/2696) (@triarius)

### Fixed
- Fix commit resolution error message [#2699](https://github.com/buildkite/agent/pull/2699) (@moskyb)
- Update outdated option name [#2693](https://github.com/buildkite/agent/pull/2693) (@fruechel-canva)

### Internal
- Add a User-Agent header when uploading artifacts to Buildkite's default location [#2672](https://github.com/buildkite/agent/pull/2672) (@yob)
- Break from artifact upload retry loop on more 4xx responses [#2697](https://github.com/buildkite/agent/pull/2697) (@SorchaAbel)
- Use roko.DoFunc [#2689](https://github.com/buildkite/agent/pull/2689) (@DrJosh9000)
- Dependabot up to its usual tricks: [#2704](https://github.com/buildkite/agent/pull/2704), [#2701](https://github.com/buildkite/agent/pull/2701), [#2702](https://github.com/buildkite/agent/pull/2702), [#2666](https://github.com/buildkite/agent/pull/2666), [#2691](https://github.com/buildkite/agent/pull/2691), [#2692](https://github.com/buildkite/agent/pull/2692)

## [v3.66.0](https://github.com/buildkite/agent/tree/v3.66.0) (2024-03-12)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.65.0...v3.66.0)

### Added
- Extend graceful cancellation to all job phases [#2654](https://github.com/buildkite/agent/pull/2654) (@david-poirier)
- Add cli command to redact secrets and redact secrets from Pipelines Secrets [#2660](https://github.com/buildkite/agent/pull/2660) (@triarius)
- Configurably optional warnings [#2674](https://github.com/buildkite/agent/pull/2674) (@moskyb)

### Fixed
- Update `tool sign` usage description to match actual command [#2677](https://github.com/buildkite/agent/pull/2677) (@CheeseStick)
- Remove experimental callout on signing flags (it wasn't experimental) [#2668](https://github.com/buildkite/agent/pull/2668) (@moskyb)

### Changed
- Promote `avoid-recursive-trap` experiment [#2669](https://github.com/buildkite/agent/pull/2669) (@triarius)
- Remove requests logging in the Job API unless if in debug mode [#2662](https://github.com/buildkite/agent/pull/2662) (@triarius)
- Force GitHub URLs to use HTTPS if the agent's git-credential-helper if it is enabled [#2655](https://github.com/buildkite/agent/pull/2655) (@triarius)

### Internal
- @dependabot's been hard at work: [#2681](https://github.com/buildkite/agent/pull/2681) [#2686](https://github.com/buildkite/agent/pull/2686) [#2679](https://github.com/buildkite/agent/pull/2679) [#2685](https://github.com/buildkite/agent/pull/2685) [#2682](https://github.com/buildkite/agent/pull/2682) [#2678](https://github.com/buildkite/agent/pull/2678) [#2680](https://github.com/buildkite/agent/pull/2680) [#2684](https://github.com/buildkite/agent/pull/2684)
- Update mime types [#2661](https://github.com/buildkite/agent/pull/2661) (@triarius)

## [v3.65.0](https://github.com/buildkite/agent/tree/v3.65.0) (2024-02-23)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.64.0...v3.65.0)

### Added
- Add flag for setting annotation priority [#2644](https://github.com/buildkite/agent/pull/2644) (@matthewborden)

### Changed
- Chill out credential helper logging [#2650](https://github.com/buildkite/agent/pull/2650) (@moskyb)

### Internal
- Fix test of JobAPI requiring socket set [#2651](https://github.com/buildkite/agent/pull/2651) (@triarius)

## [v3.64.0](https://github.com/buildkite/agent/tree/v3.64.0) (2024-02-21)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.63.1...v3.64.0)

### Added
- De-experimentify Job API [#2646](https://github.com/buildkite/agent/pull/2646) (@triarius)
- Add explicit queue flag to the agent [#2648](https://github.com/buildkite/agent/pull/2648) (@moskyb)
- Add an info log of which experiments are known and enabled on agent start [#2645](https://github.com/buildkite/agent/pull/2645) (@triarius)
- Add cli command to read from Pipelines Secrets [Not available to customers yet] [#2647](https://github.com/buildkite/agent/pull/2647) (@triarius)

### Fixed
- YAML marshaling of `wait`, `block`, and `input` scalar steps (when using `tool sign` or `pipeline upload --format=yaml`) [#2640](https://github.com/buildkite/agent/pull/2640) (@DrJosh9000)
- Packaging: Use separate repos for each package type [#2636](https://github.com/buildkite/agent/pull/2636) (@sj26)

### Internal
- Various dependency updates: [#2643](https://github.com/buildkite/agent/pull/2643), [#2642](https://github.com/buildkite/agent/pull/2642) [#2641](https://github.com/buildkite/agent/pull/2641), [#2638](https://github.com/buildkite/agent/pull/2638), [#2640](https://github.com/buildkite/agent/pull/2640), [#2639](https://github.com/buildkite/agent/pull/2639), [#2637](https://github.com/buildkite/agent/pull/2637)

## [v3.63.1](https://github.com/buildkite/agent/tree/v3.63.1) (2024-02-16)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.63.0...v3.63.1)

### Fixed
- Fix NPE when decoding token response [#2634](https://github.com/buildkite/agent/pull/2634) (@moskyb)

## [v3.63.0](https://github.com/buildkite/agent/tree/v3.63.0) (2024-02-14)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.62.0...v3.63.0)

> [!WARNING]
> This release has two potentially breaking changes in the way environment
> variables are interpolated.

* Interpolation on Windows should be done in a case-_in_sensitive manner to be
  compatible with Batch scripts and Powershell. This was working correctly up
  until some refactoring in v3.59.0.

  For example, this pipeline:

  ```yaml
  env:
    FOO: bar
  steps:
  - command: echo $Foo $FOO
  ```

  should now be correctly interpolated on Windows as:

  ```yaml
  env:
    FOO: bar
  steps:
  - command: echo bar bar
  ```

  Interpolation on other platforms is unchanged.

* Our [documented interpolation rules](https://buildkite.com/docs/pipelines/environment-variables#environment-variable-precedence)
  implies that variables from the agent environment have higher precedence than
  variables defined by the job environment ("we merge in some of the variables
  from the agent environment").

  Suppose the agent environment contains `FOO=runtime_foo`. The pipeline

  ```yaml
  env:
    BAR: $FOO
    FOO: pipeline_foo
  steps:
  - command: echo hello world
  ```

  would in previous releases be interpolated as:

  ```yaml
  env:
    BAR: runtime_foo
    FOO: pipeline_foo
  steps:
  - command: echo hello world
  ```

  On the other hand, the pipeline

  ```yaml
  env:
    FOO: pipeline_foo
    BAR: $FOO
  steps:
  - command: echo hello world
  ```

  would be interpolated to become

  ```yaml
  env:
    FOO: pipeline_foo
    BAR: pipeline_foo
  steps:
  - command: echo hello world
  ```

  We think this is inconsistent with the agent environment taking precedence,
  and if users would like to interpolate `$FOO` as the value of the pipeline
  level definition of `FOO`, they should ensure the agent environment does not
  contain `FOO`.

### Added
- BK github app git credentials helper [#2599](https://github.com/buildkite/agent/pull/2599) (@moskyb)

### Fixed
- Fix pipeline interpolation case sensitivity on Windows, and runtime environment variable precedence [#2624](https://github.com/buildkite/agent/pull/2624) (@triarius)
- Fix environment variable changes in hooks logged incorrectly [#2621](https://github.com/buildkite/agent/pull/2621) (@triarius)
- Fix Powershell hooks on windows [#2613](https://github.com/buildkite/agent/pull/2613) (@triarius)
- Fix bug where unauthorised register was retrying erroneously [#2614](https://github.com/buildkite/agent/pull/2614) (@moskyb)
- Fix docs for --allowed-environment-variables [#2598](https://github.com/buildkite/agent/pull/2598) (@tessereth)

### Upgraded
- The agent is now built with Go 1.22 [#2631](https://github.com/buildkite/agent/pull/2631) (@moskyb)

### Internal
- Add a PR template [#2601](https://github.com/buildkite/agent/pull/2601) (@triarius)
- Move check from upload-release-steps.sh to pipeline.yml [#2617](https://github.com/buildkite/agent/pull/2617) (@DrJosh9000)
- build-github-release.sh tidyups [#2619](https://github.com/buildkite/agent/pull/2619) (@DrJosh9000)
- Various dependency updates [#2625](https://github.com/buildkite/agent/pull/2625), [#2630](https://github.com/buildkite/agent/pull/2630), [#2627](https://github.com/buildkite/agent/pull/2627), [#2626](https://github.com/buildkite/agent/pull/2626), [#2622](https://github.com/buildkite/agent/pull/2622), [#2605](https://github.com/buildkite/agent/pull/2605), [#2609](https://github.com/buildkite/agent/pull/2609), [#2603](https://github.com/buildkite/agent/pull/2603), [#2602](https://github.com/buildkite/agent/pull/2602), [#2604](https://github.com/buildkite/agent/pull/2604), [#2606](https://github.com/buildkite/agent/pull/2606), [#2616](https://github.com/buildkite/agent/pull/2616), [#2610](https://github.com/buildkite/agent/pull/2610), [#2611](https://github.com/buildkite/agent/pull/2611)

## [v3.62.0](https://github.com/buildkite/agent/tree/v3.62.0) (2024-01-23)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.61.0...v3.62.0)

### Added
- Add more fields to job logger [#2578](https://github.com/buildkite/agent/pull/2578) (@ChrisBr)
- Environment Variable allowlisting [#2539](https://github.com/buildkite/agent/pull/2539) (@moskyb, originally @CheeseStick)

### Fixed
- When the server returns a 401, stop retrying and bail out [#2569](https://github.com/buildkite/agent/pull/2569) (@SorchaAbel)
- Retry for 24 hours instead of forever [#2588](https://github.com/buildkite/agent/pull/2588) (@tessereth)
- Documentation updates [#2590](https://github.com/buildkite/agent/pull/2590) (@moskyb), [#2591](https://github.com/buildkite/agent/pull/2591) (@moskyb), [#2589](https://github.com/buildkite/agent/pull/2589) (@moskyb)

### Internal
- Various @dependabot[bot] updates [#2587](https://github.com/buildkite/agent/pull/2587), [#2594](https://github.com/buildkite/agent/pull/2594), [#2596](https://github.com/buildkite/agent/pull/2596), [#2595](https://github.com/buildkite/agent/pull/2595), [#2593](https://github.com/buildkite/agent/pull/2593), [#2592](https://github.com/buildkite/agent/pull/2592), [#2585](https://github.com/buildkite/agent/pull/2585), [#2584](https://github.com/buildkite/agent/pull/2584), [#2583](https://github.com/buildkite/agent/pull/2583), [#2573](https://github.com/buildkite/agent/pull/2573), [#2582](https://github.com/buildkite/agent/pull/2582), [#2572](https://github.com/buildkite/agent/pull/2572), [#2571](https://github.com/buildkite/agent/pull/2571), [#2575](https://github.com/buildkite/agent/pull/2575), [#2580](https://github.com/buildkite/agent/pull/2580), [#2567](https://github.com/buildkite/agent/pull/2567), [#2566](https://github.com/buildkite/agent/pull/2566), [#2563](https://github.com/buildkite/agent/pull/2563), [#2562](https://github.com/buildkite/agent/pull/2562), [#2564](https://github.com/buildkite/agent/pull/2564), [#2565](https://github.com/buildkite/agent/pull/2565)

## [v3.61.0](https://github.com/buildkite/agent/tree/v3.61.0) (2023-12-14)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.60.1...v3.61.0)

### Added
- Add more debug logging and error wrapping for running processes [#2543](https://github.com/buildkite/agent/pull/2543) (@triarius)
- Enable overriding buildkite-agent url in `install.ps1` [#1805](https://github.com/buildkite/agent/pull/1805) (@staticfloat)

### Fixed
- Buildkite build script is broken due to missing version default value [#2559](https://github.com/buildkite/agent/pull/2559) (@amir-khatibzadeh)
- Update go-pipeline to v0.3.2 (fixes parsing pipelines that contain YAML aliases used as mapping keys) [#2560](https://github.com/buildkite/agent/pull/2560) (@DrJosh9000)

### Changed
- Alpine image updated from 3.18.5 to 3.19.0 [#2545](https://github.com/buildkite/agent/pull/2545), [#2549](https://github.com/buildkite/agent/pull/2549), [#2550](https://github.com/buildkite/agent/pull/2550), [#2551](https://github.com/buildkite/agent/pull/2551)

### Internal
- Make it clear these are not leaked credentials [#2554](https://github.com/buildkite/agent/pull/2554) (@sj26)
- Various other @dependabot[bot] updates [#2553](https://github.com/buildkite/agent/pull/2553), [#2544](https://github.com/buildkite/agent/pull/2544), [#2548](https://github.com/buildkite/agent/pull/2548), [#2552](https://github.com/buildkite/agent/pull/2552), [#2547](https://github.com/buildkite/agent/pull/2547)


## [v3.60.1](https://github.com/buildkite/agent/tree/v3.60.1) (2023-12-06)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.60.0...v3.60.1)

### Security
- Bump docker/library/golang from 1.21.4 to 1.21.5 in /.buildkite [#2542](https://github.com/buildkite/agent/pull/2542)

### Fixed
- Fix typo in environment variable name for allowed-plugins [#2526](https://github.com/buildkite/agent/pull/2526) (@moskyb)
- Fix environment variable interpolation into command step labels [#2540](https://github.com/buildkite/agent/pull/2540) (@triarius)

### Internal
- Refactor hook wrapper writing [#2505](https://github.com/buildkite/agent/pull/2505) (@triarius)
- Use os.RemoveAll in cleanup [#2538](https://github.com/buildkite/agent/pull/2538) (@DrJosh9000)
- Dependencies [#2537](https://github.com/buildkite/agent/pull/2537) [#2536](https://github.com/buildkite/agent/pull/2536) [#2500](https://github.com/buildkite/agent/pull/2500) [#2528](https://github.com/buildkite/agent/pull/2528) [#2529](https://github.com/buildkite/agent/pull/2529) [#2533](https://github.com/buildkite/agent/pull/2533) [#2532](https://github.com/buildkite/agent/pull/2532) [#2534](https://github.com/buildkite/agent/pull/2534) [#2535](https://github.com/buildkite/agent/pull/2535)


## [v3.60.0](https://github.com/buildkite/agent/tree/v3.60.0) (2023-11-29)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.59.0...v3.60.0)

Signed pipelines is now GA! Check out the docs [here](https://buildkite.com/docs/agent/v3/signed-pipelines) if you want a little more zero-trust mixed into your pipelines.

### Added
- Signed Pipelines goes GA! 🎉 [#2492](https://github.com/buildkite/agent/pull/2492) [#2521](https://github.com/buildkite/agent/pull/2521) [#2522](https://github.com/buildkite/agent/pull/2522) (@moskyb + @triarius)

### Changed
- Insert extra timestamps after a timeout [#2447](https://github.com/buildkite/agent/pull/2447) (@DrJosh9000)
- Log the max size warning once [#2497](https://github.com/buildkite/agent/pull/2497) (@DrJosh9000)
- MetaDataSetCommand: retry longer (exponential backoff) [#2514](https://github.com/buildkite/agent/pull/2514) (@pda)
- Humanize bytes to IEC (1024 → KiB etc) not SI (1000 → KB etc) [#2513](https://github.com/buildkite/agent/pull/2513) (@pda)

### Internal
- More log streamer cleanups [#2498](https://github.com/buildkite/agent/pull/2498) (@DrJosh9000)
- Add a helpful note to security researchers [#2520](https://github.com/buildkite/agent/pull/2520) (@DrJosh9000)
- Update Go to 1.21 [#2284](https://github.com/buildkite/agent/pull/2284) (@triarius + @moskyb)
- Dependabot's making us all look bad at our jobs: [#2501](https://github.com/buildkite/agent/pull/2501) [#2499](https://github.com/buildkite/agent/pull/2499) [#2515](https://github.com/buildkite/agent/pull/2515) [#2509](https://github.com/buildkite/agent/pull/2509) [#2502](https://github.com/buildkite/agent/pull/2502) [#2516](https://github.com/buildkite/agent/pull/2516) [#2517](https://github.com/buildkite/agent/pull/2517) [#2496](https://github.com/buildkite/agent/pull/2496) [#2493](https://github.com/buildkite/agent/pull/2493) [#2495](https://github.com/buildkite/agent/pull/2495) [#2494](https://github.com/buildkite/agent/pull/2494) [#2504](https://github.com/buildkite/agent/pull/2504)

## [v3.59.0](https://github.com/buildkite/agent/tree/v3.59.0) (2023-11-09)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.58.0...v3.59.0)

### Security
- This release is built with Go 1.20.11, which includes fixes for two vulnerabilities in file path handling on Windows (CVE-2023-45283, CVE-2023-45284). [#2486](https://github.com/buildkite/agent/pull/2486)

### Changed
- Experimental: Signed Pipelines
  - Allow omitting the key ID when signing pipelines [#2481](https://github.com/buildkite/agent/pull/2481) (@triarius)
  - Remove Org and Pipeline slugs from pipeline invariants and update the signing tool to use the GraphQL API [#2479](https://github.com/buildkite/agent/pull/2479) (@triarius)
  - Add key.Validate call [#2488](https://github.com/buildkite/agent/pull/2488) (@DrJosh9000)
- Use zzglob.MultiGlob to process multiple globs simultaneously, and stop sending GlobPath with artifact upload [#2472](https://github.com/buildkite/agent/pull/2472) (@DrJosh9000)

### Internal
- Migrate usage of internal/{pipeline,ordered,jwkutil} to go-pipeline [#2489](https://github.com/buildkite/agent/pull/2489) (@moskyb)
- Update bintest to v3.2.0 to resolve ETXTBSY race condition in tests [#2480](https://github.com/buildkite/agent/pull/2480) (@DrJosh9000)
- Fix race in header times streamer [#2485](https://github.com/buildkite/agent/pull/2485), [#2487](https://github.com/buildkite/agent/pull/2487) (@DrJosh9000)
- Various dependency updates [#2484](https://github.com/buildkite/agent/pull/2484), [#2482](https://github.com/buildkite/agent/pull/2482)

## [v3.58.0](https://github.com/buildkite/agent/tree/v3.58.0) (2023-11-02)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.57.0...v3.58.0)

### Added
- Add allowed-plugin param to enable plugins allow-list [#2471](https://github.com/buildkite/agent/pull/2471) (@jakubm-canva)
- New experiment: `pty-raw` avoids LF→CRLF mapping by setting PTY to raw mode [#2453](https://github.com/buildkite/agent/pull/2453) (@pda)
- Experimental: Signed Pipelines
  - Add some pipeline invariants to the signature and create a cli subcommand to sign a pipeline [#2457](https://github.com/buildkite/agent/pull/2457) (@triarius)
  - Add log group headers and timestamps to job verification success and failure logs [#2461](https://github.com/buildkite/agent/pull/2461) (@triarius)

### Fixed
- Fix checkout of short commit hashes [#2465](https://github.com/buildkite/agent/pull/2465) (@triarius)
- Parallelise artifact collection [#2456](https://github.com/buildkite/agent/pull/2456) (@DrJosh9000), [#2477](https://github.com/buildkite/agent/pull/2477) (@DrJosh9000)
- Log warning about short vars once [#2454](https://github.com/buildkite/agent/pull/2454) (@DrJosh9000)

### Internal
- Reduce header regexps [#2135](https://github.com/buildkite/agent/pull/2135) (@DrJosh9000)
- Various dependency updates: [#2469](https://github.com/buildkite/agent/pull/2469), [#2468](https://github.com/buildkite/agent/pull/2468), [#2467](https://github.com/buildkite/agent/pull/2467), [#2463](https://github.com/buildkite/agent/pull/2463), [#2450](https://github.com/buildkite/agent/pull/2450), [#2460](https://github.com/buildkite/agent/pull/2460), [#2459](https://github.com/buildkite/agent/pull/2459), [#2458](https://github.com/buildkite/agent/pull/2458)

## [v3.57.0](https://github.com/buildkite/agent/tree/v3.57.0) (2023-10-19)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.56.0...v3.57.0)

### Added
- Experimental: Signed Pipelines
  - Signing build matrices [#2440](https://github.com/buildkite/agent/pull/2440) [#2429](https://github.com/buildkite/agent/pull/2429) [#2426](https://github.com/buildkite/agent/pull/2426) [#2425](https://github.com/buildkite/agent/pull/2425) [#2391](https://github.com/buildkite/agent/pull/2391) [#2395](https://github.com/buildkite/agent/pull/2395) (@DrJosh9000)
  - Add debug logs for job verification [#2439](https://github.com/buildkite/agent/pull/2439) (@DrJosh9000)
  - Reduce information in verification errors [#2431](https://github.com/buildkite/agent/pull/2431) (@DrJosh9000)
  - Separate step/pipeline env vars for job validation [#2428](https://github.com/buildkite/agent/pull/2428) (@DrJosh9000)
  - Signing config cleanup [#2420](https://github.com/buildkite/agent/pull/2420) [#2427](https://github.com/buildkite/agent/pull/2427) (@moskyb)
  - Fix verifying jobs with no plugins [#2419](https://github.com/buildkite/agent/pull/2419) (@DrJosh9000)
  - Use canonicalised JSON as signature payload [#2416](https://github.com/buildkite/agent/pull/2416) (@DrJosh9000)
  - Add utility for generating signing and verification keys [#2415](https://github.com/buildkite/agent/pull/2415) [#2422](https://github.com/buildkite/agent/pull/2422) (@moskyb)

### Changed
- Revert "Upgrade pre-installed packages in docker images" and Pin docker images by digest [#2430](https://github.com/buildkite/agent/pull/2430) (@triarius)

### Internal
- Use docker image bases from ECR public gallery [#2423](https://github.com/buildkite/agent/pull/2423) [#2424](https://github.com/buildkite/agent/pull/2424) (@triarius + @moskyb)
- Add CODEOWNERS file [#2444](https://github.com/buildkite/agent/pull/2444) (@moskyb)
- Push agent packages to Packagecloud [#2438](https://github.com/buildkite/agent/pull/2438) [#2441](https://github.com/buildkite/agent/pull/2441) [#2443](https://github.com/buildkite/agent/pull/2443) [#2442](https://github.com/buildkite/agent/pull/2442) (@sj26)
- Test clicommand config completeness [#2414](https://github.com/buildkite/agent/pull/2414) (@moskyb)
- As always, the cosmic background radiation of dependabot updates. Thanks dependabot! [#2435](https://github.com/buildkite/agent/pull/2435) [#2434](https://github.com/buildkite/agent/pull/2434) [#2433](https://github.com/buildkite/agent/pull/2433) [#2432](https://github.com/buildkite/agent/pull/2432) [#2421](https://github.com/buildkite/agent/pull/2421) [#2418](https://github.com/buildkite/agent/pull/2418) [#2417](https://github.com/buildkite/agent/pull/2417)

## [v3.56.0](https://github.com/buildkite/agent/tree/v3.56.0) (2023-10-05)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.55.0...v3.56.0)

### Security
- Upgrade libc packages in Ubuntu 22.04 docker image to those patched for [CVE-2023-4911](https://ubuntu.com/security/CVE-2023-4911) [#2410](https://github.com/buildkite/agent/pull/2410) (@triarius)

### Added
- Add `allow-repositories` param to enable repository allow-listing [#2361](https://github.com/buildkite/agent/pull/2361) (@david-poirier)

### Changed
- Upgrade pre-installed packages in docker images [#2410](https://github.com/buildkite/agent/pull/2410) (@triarius)
- Add Matrix parsing [#2382](https://github.com/buildkite/agent/pull/2382) (@DrJosh9000)
- Add `EXPERIMENTAL:` to the help text for all pipeline signing flags [#2412](https://github.com/buildkite/agent/pull/2412) (@moskyb)

### Fixed
- Fix parsing pipelines what use a string as the skip key in a matrix adjustment [#2407](https://github.com/buildkite/agent/pull/2407) (@moskyb)

### Internal
- Fix flaky TestLockFileRetriesAndTimesOut [#2392](https://github.com/buildkite/agent/pull/2392) (@DrJosh9000)
- Fix apt install awscli [#2390](https://github.com/buildkite/agent/pull/2390) (@moskyb)
- Fix incorrect check in a test 😅 [#2381](https://github.com/buildkite/agent/pull/2381) (@DrJosh9000)
- Run createrepo_c on ubuntu [#2385](https://github.com/buildkite/agent/pull/2385) [#2389](https://github.com/buildkite/agent/pull/2389) (@moskyb)
- Update dependabot config to use groups [#2384](https://github.com/buildkite/agent/pull/2384) (@moskyb)
- Fix some typos in code comments [#2380](https://github.com/buildkite/agent/pull/2380) (@testwill)

And (a slightly larger?) than usual amount of  updates [#2369](https://github.com/buildkite/agent/pull/2369) [#2371](https://github.com/buildkite/agent/pull/2371) [#2372](https://github.com/buildkite/agent/pull/2372) [#2373](https://github.com/buildkite/agent/pull/2373) [#2377](https://github.com/buildkite/agent/pull/2377) [#2378](https://github.com/buildkite/agent/pull/2378) [#2383](https://github.com/buildkite/agent/pull/2383) [#2386](https://github.com/buildkite/agent/pull/2386) [#2387](https://github.com/buildkite/agent/pull/2387) [#2397](https://github.com/buildkite/agent/pull/2397) [#2398](https://github.com/buildkite/agent/pull/2398) [#2399](https://github.com/buildkite/agent/pull/2399) [#2400](https://github.com/buildkite/agent/pull/2400) [#2401](https://github.com/buildkite/agent/pull/2401) [#2402](https://github.com/buildkite/agent/pull/2402) [#2403](https://github.com/buildkite/agent/pull/2403) [#2405](https://github.com/buildkite/agent/pull/2405)


## [v3.55.0](https://github.com/buildkite/agent/tree/v3.55.0) (2023-09-14)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.54.0...v3.55.0)

### Fixed
- Annotations created with contexts that contain `.` can now be removed [#2365](https://github.com/buildkite/agent/pull/2365) (@DrJosh9000)

### Changed
- Add a full agent version which includes the commit [#2283](https://github.com/buildkite/agent/pull/2283) (@triarius)

## [v3.54.0](https://github.com/buildkite/agent/tree/v3.54.0) (2023-09-05)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.53.0...v3.54.0)

> ⚠️ We're adjusting how the set of supported OS versions changes over time.
> For the details, see [#2354](https://github.com/buildkite/agent/pull/2354).

### Added
- New experiment `use-zzglob`: uses a different library for resolving glob patterns in `buildkite-agent artifact upload` [#2341](https://github.com/buildkite/agent/pull/2341) (@DrJosh9000)

### Changed
- Logged errors might look different: errors passed back up to main.go from clicommand [#2347](https://github.com/buildkite/agent/pull/2347) (@triarius)
- HEAD commit found faster: `git log` is now used to get commit information instead of `git show` [#2323](https://github.com/buildkite/agent/pull/2323) (@leakingtapan)

### Internal
- Adapt Olfactor to allow sniffing for multiple smells [#2332](https://github.com/buildkite/agent/pull/2332) (@triarius)

## [v3.53.0](https://github.com/buildkite/agent/tree/v3.53.0) (2023-08-31)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.52.1...v3.53.0)

### Added
- Artifact upload and download to/from Azure Blob Storage [#2318](https://github.com/buildkite/agent/pull/2318) (@DrJosh9000)

### Fixed
- Fix detection of missing commits on checkout [#2322](https://github.com/buildkite/agent/pull/2322) (@goodspark)
- [Experimental] Handle the case when unmarshalling a step where there aren't any plugins [#2321](https://github.com/buildkite/agent/pull/2321) (@moskyb)
- [Experimental] Fix signature mismatches when steps have plugins [#2339](https://github.com/buildkite/agent/pull/2339), [#2319](https://github.com/buildkite/agent/pull/2319) (@DrJosh9000)
- [Experimental] Catch step env/job env edge case [#2340](https://github.com/buildkite/agent/pull/2340) (@DrJosh9000)

### Changed
- Retry fork/exec errors when running hook [#2325](https://github.com/buildkite/agent/pull/2325) (@triarius)

### Internal
- Fix ECR authentication failure [#2337](https://github.com/buildkite/agent/pull/2337), [#2335](https://github.com/buildkite/agent/pull/2335), [#2334](https://github.com/buildkite/agent/pull/2334) (@DrJosh9000)
- Split checkout, artifact, and plugin phases out of executor.go [#2324](https://github.com/buildkite/agent/pull/2324) (@triarius)
- Store experiments in contexts [#2316](https://github.com/buildkite/agent/pull/2316) (@DrJosh9000)

## [v3.52.1](https://github.com/buildkite/agent/tree/v3.52.1) (2023-08-23)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.52.0...v3.52.1)

### Fixed
- Fix missing group interpolation [#2303](https://github.com/buildkite/agent/pull/2303) (@DrJosh9000)
- Experimental fix for agent workers reading plugin directories while they are being written to by other agent workers [#2301](https://github.com/buildkite/agent/pull/2301) (@triarius)

### Internal
- Rework method of pushing releases to RPM repos [#2315](https://github.com/buildkite/agent/pull/2315) [#2314](https://github.com/buildkite/agent/pull/2314) [#2312](https://github.com/buildkite/agent/pull/2312) [#2310](https://github.com/buildkite/agent/pull/2310) [#2304](https://github.com/buildkite/agent/pull/2304) (@DrJosh9000)
- Update help text with suggestions from docs code review [#2313](https://github.com/buildkite/agent/pull/2313) (@triarius)
- Fix a flaky shell test [#2311](https://github.com/buildkite/agent/pull/2311) (@triarius)
- Adjust cli help output to work better with documentation generation [#2317](https://github.com/buildkite/agent/pull/2317) (@triarius)

## [v3.52.0](https://github.com/buildkite/agent/tree/v3.52.0) (2023-08-17)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.51.0...v3.52.0)

### Added
- [Experimental] Include pipeline and step env in step signatures [#2295](https://github.com/buildkite/agent/pull/2295) (@DrJosh9000)

### Fixed
- Fix step get is printing the address of the stdout stream at the start [#2299](https://github.com/buildkite/agent/pull/2299) (@triarius)

### Changed
- Add a newline after printing errors from the config parser [#2296](https://github.com/buildkite/agent/pull/2296) (@triarius)

### Internal
- Enable mount-buildkite-agent in release pipeline containers [#2298](https://github.com/buildkite/agent/pull/2298) (@DrJosh9000)
- Update ecr, docker plugins, and agent image ver [#2297](https://github.com/buildkite/agent/pull/2297) (@DrJosh9000)
- Pin bk cli used in agent pipeline to a commit [#2294](https://github.com/buildkite/agent/pull/2294) (@triarius)

## [v3.51.0](https://github.com/buildkite/agent/tree/v3.51.0) (2023-08-15)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.50.4...v3.51.0)

### Added
- Add --strict-single-hooks [#2268](https://github.com/buildkite/agent/pull/2268) (@DrJosh9000)
- Add missing 'an' in annotation help [#2285](https://github.com/buildkite/agent/pull/2285) (@mdb)
- [Experimental] Verify step signatures [#2210](https://github.com/buildkite/agent/pull/2210) (@moskyb)
- [Experimental] Pipeline Signing/Verification with JWS [#2252](https://github.com/buildkite/agent/pull/2252) (@moskyb)
- [Experimental] Include plugins in command step signatures [#2292](https://github.com/buildkite/agent/pull/2292) (@DrJosh9000)

### Changed
- Make the agent send a SIGTERM (configurable) before a SIGKILL to subprocesses [#2250](https://github.com/buildkite/agent/pull/2250) (@triarius)
- Limit job log length [#2192](https://github.com/buildkite/agent/pull/2192) (@DrJosh9000)
- Refactor redactor into streaming replacer and use it to redact secrets [#2277](https://github.com/buildkite/agent/pull/2277) (@DrJosh9000)
- Dependency upgrades [#2278](https://github.com/buildkite/agent/pull/2278) [#2274](https://github.com/buildkite/agent/pull/2274) [#2271](https://github.com/buildkite/agent/pull/2271) [#2272](https://github.com/buildkite/agent/pull/2272) [#2275](https://github.com/buildkite/agent/pull/2275) [#2266](https://github.com/buildkite/agent/pull/2266)

### Fixed
- Fix `fatal: bad object` not detected from git fetch [#2286](https://github.com/buildkite/agent/pull/2286) (@triarius)
- Fix scalar plugin parsing [#2264](https://github.com/buildkite/agent/pull/2264) (@DrJosh9000)

### Internal
- Reorganise step types among files [#2267](https://github.com/buildkite/agent/pull/2267) (@DrJosh9000)
- Upload test coverage [#2270](https://github.com/buildkite/agent/pull/2270) (@DrJosh9000)
- Remove unwrapping in error `Is` methods [#2269](https://github.com/buildkite/agent/pull/2269) (@triarius)
- Use capacity hint in `concat` [#2288](https://github.com/buildkite/agent/pull/2288) (@DrJosh9000)
- Add ordered.Unmarshal, and use it in pipeline parsing [#2279](https://github.com/buildkite/agent/pull/2279) (@DrJosh9000)
- Create a setup method for config and logger to reduce boilerplate [#2281](https://github.com/buildkite/agent/pull/2281) (@triarius)
- Add retry for publishing RPMs [#2280](https://github.com/buildkite/agent/pull/2280) (@triarius)
- Fix data race in testAgentEndpoint [#2265](https://github.com/buildkite/agent/pull/2265) (@DrJosh9000)
- Fix missing "fmt" import [#2287](https://github.com/buildkite/agent/pull/2287) (@DrJosh9000)

## [v3.50.4](https://github.com/buildkite/agent/tree/v3.50.4) (2023-07-31)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.50.3...v3.50.4)

### Fixed
- Even More Pipeline Parsing Fixes [#2253](https://github.com/buildkite/agent/pull/2253) (@moskyb)
- Fix missing `return` statements when unmarshalling fails [#2245](https://github.com/buildkite/agent/pull/2245) (@moskyb), [#2257](https://github.com/buildkite/agent/pull/2257) (@DrJosh9000)
- Add future-proofing `UnknownStep` type [#2254](https://github.com/buildkite/agent/pull/2254) (@DrJosh9000)
- Nil handling fixes, particularly parsing `env: null` [#2260](https://github.com/buildkite/agent/pull/2260) (@DrJosh9000)

## Changed
- Remove docker-compose v1 from ubuntu 22.04 and replace with compatibility script [#2248](https://github.com/buildkite/agent/pull/2248) (@triarius)
- Authentication failure errors when using S3 now mention `BUILDKITE_S3_PROFILE` and `AWS_PROFILE` [#2247](https://github.com/buildkite/agent/pull/2247) (@DrJosh9000)

## Internal
- Remove a double check for the existence of a local hook and log when it is missing in debug [#2249](https://github.com/buildkite/agent/pull/2249) (@triarius)
- Refactor some code in process.go [#2251](https://github.com/buildkite/agent/pull/2251) (@triarius)
- Store `GOCACHE` outside container [#2256](https://github.com/buildkite/agent/pull/2256) (@DrJosh9000)
- Get mime types from github, rather than Apache's SVN Server [#2255](https://github.com/buildkite/agent/pull/2255) (@moskyb)
- Check that go.mod is tidy in CI [#2246](https://github.com/buildkite/agent/pull/2246) (@moskyb) and fix flakiness of this check [#2261](https://github.com/buildkite/agent/pull/2261) (@triarius)

## [v3.50.3](https://github.com/buildkite/agent/tree/v3.50.3) (2023-07-24)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.50.2...v3.50.3)

### Changed
- Two-phase pipeline parsing [#2238](https://github.com/buildkite/agent/pull/2238) (@DrJosh9000)
- Remove installing qemu-binfmt from agent pipeline [#2236](https://github.com/buildkite/agent/pull/2236) (@triarius)

## [v3.50.2](https://github.com/buildkite/agent/tree/v3.50.2) (2023-07-21)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.50.1...v3.50.2)

This release contains a known issue:
|Severity|Description|Fixed in|
|---|---|---|
| Medium | When uploading pipelines, if any object in the pipeline YAML contained multiple merge keys, the pipeline would fail to parse. See below for a workaround | **✅ Fixed in [v3.50.3](#v3.50.3)** |

### Fixed
- Fix an issue introduced in [#2207](https://github.com/buildkite/agent/pull/2207) where jobs wouldn't check if they'd been cancelled [#2231](https://github.com/buildkite/agent/pull/2231) (@triarius)
- Fix avoid-recursive-trap experiment not recognised [#2235](https://github.com/buildkite/agent/pull/2235) (@triarius)
- Further refactor to `agent.JobRunner` [#2222](https://github.com/buildkite/agent/pull/2222) [#2230](https://github.com/buildkite/agent/pull/2230) (@moskyb)


## [v3.50.1](https://github.com/buildkite/agent/tree/v3.50.1) (2023-07-20)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.49.0...v3.50.1)

This release contains multiple issues:

|Severity|Description|Fixed in|
|---|---|---|
| ⚠️ Very High | Jobs running on this version of the agent are not cancellable from the UI/API | **✅ Fixed in [v3.50.2](#v3.50.2)** |
| Medium | When uploading pipelines, if any object in the pipeline YAML contained multiple merge keys, the pipeline would fail to parse. See below for a workaround | **✅ Fixed in [v3.50.3](#v3.50.3)** |

### Fixed
- Empty or zero-length `steps` is no longer a parser error, and is normalised to \[\] instead [#2225](https://github.com/buildkite/agent/pull/2225), [#2229](https://github.com/buildkite/agent/pull/2229) (@DrJosh9000)
- Group steps now correctly include the `group` key [#2226](https://github.com/buildkite/agent/pull/2226) (@DrJosh9000)
- Increases to test coverage for the new parser [#2227](https://github.com/buildkite/agent/pull/2227) (@DrJosh9000)

## [v3.50.0](https://github.com/buildkite/agent/tree/v3.50.0) (2023-07-18)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.49.0...v3.50.0)

This release contains multiple issues:

|Severity|Description|Fixed in|
|---|---|---|
| Medium | When uploading pipelines, some group steps are not correctly parsed, and were ignored. | **✅ Fixed in [v3.50.1](#v3.50.1)** |
| Low | Uploading pipelines with empty or zero-length `steps` failed, where they should've been a no-op. | **✅ Fixed in [v3.50.1](#v3.50.1)** |
| ⚠️ Very High | Jobs running on this version of the agent are not cancellable from the UI/API | **✅ Fixed in [v3.50.2](#v3.50.2)** |
| Medium | When uploading pipelines, if any object in the pipeline YAML contained multiple merge keys, the pipeline would fail to parse. See below for a workaround | **✅ Fixed in [v3.50.3](#v3.50.3)** |


<details>
<summary>Workaround for yaml merge key issue</summary>
For example, this pipeline would fail to parse:

```yaml
default_plugins: &default_plugins
  plugins:
    - docker#4.0.0:
        image: alpine:3.14

default_retry: &default_retry
  retry:
    automatic:
      - exit_status: 42

steps:
  - <<: *default_plugins
    <<: *default_retry
    command: "echo 'hello, world!'"
```

As a workaround for this, you can use yaml array merge syntax instead:

```yaml
default_plugins: &default_plugins
  plugins:
    - docker#4.0.0:
        image: alpine:3.14

default_retry: &default_retry
  retry:
    automatic:
      - exit_status: 42

steps:
  - <<: [*default_plugins, *default_retry]
    command: "echo 'hello, world!'"
```
</details>

### Added
- We're working on making pipeline signing a feature of the agent! But it's definitely not ready for primetime yet... [#2216](https://github.com/buildkite/agent/pull/2216), [#2200](https://github.com/buildkite/agent/pull/2200), [#2191](https://github.com/buildkite/agent/pull/2191), [#2186](https://github.com/buildkite/agent/pull/2186), [#2190](https://github.com/buildkite/agent/pull/2190), [#2181](https://github.com/buildkite/agent/pull/2181), [#2184](https://github.com/buildkite/agent/pull/2184), [#2173](https://github.com/buildkite/agent/pull/2173), [#2180](https://github.com/buildkite/agent/pull/2180) (@moskyb, @DrJosh9000)
- Add option to configure location of Job Log tmp file [#2174](https://github.com/buildkite/agent/pull/2174) (@yhartanto)
- Add `avoid-recursive-trap` experiment to avoid a recursive trap [#2209](https://github.com/buildkite/agent/pull/2209) (@triarius)
- Load the AWS Shared Credentials for s3 operations [#1730](https://github.com/buildkite/agent/pull/1730) (@lox)

### Fixed
- Add workaround for `fatal: bad object` errors when fetching from a git mirror [#2218](https://github.com/buildkite/agent/pull/2218) (@DrJosh9000)
- Fix missing fetch when updating git mirrors of submodules (https://github.com/buildkite/agent/pull/2203) (@DrJosh9000)
- Use a unique name for each agent started using the systemd template unit file [#2205](https://github.com/buildkite/agent/pull/2205) (@DavidGregory084)
- Polyglot hooks wasn't documented in EXPERIMENTS.md, so we fixed that [#2169](https://github.com/buildkite/agent/pull/2169) (@moskyb)
- De-experimentify wording on the status page [#2172](https://github.com/buildkite/agent/pull/2172) (@DrJosh9000)
- The secrets redactor now properly redacts multi-line secrets and overlapping secrets [#2154](https://github.com/buildkite/agent/pull/2154) (@DrJosh9000)

### Changed
- Print agent version and build in debug logs [#2211](https://github.com/buildkite/agent/pull/2211) (@triarius)
- Include the version each experiment was promoted [#2199](https://github.com/buildkite/agent/pull/2199) (@DrJosh9000)

### Various code cleanups and meta-fixes
- Fix docker builds for Ubuntu 22.04 [#2217](https://github.com/buildkite/agent/pull/2217) (@moskyb)
- JobRunner cleanup [#2207](https://github.com/buildkite/agent/pull/2207) (@moskyb)
- Simplify command phase [#2206](https://github.com/buildkite/agent/pull/2206) (@triarius)
- Rename `Bootstrap` struct (and friends) to `Executor` [#2188](https://github.com/buildkite/agent/pull/2188) (@moskyb)
- Upgrade docker compose plugin to v4.14, use docker compose v2 [#2189](https://github.com/buildkite/agent/pull/2189) (@moskyb)
- Rename package bootstrap -> job [#2187](https://github.com/buildkite/agent/pull/2187) (@moskyb)
- Clarify code around creating a process group [#2185](https://github.com/buildkite/agent/pull/2185) (@triarius)
- Fix docker builds for Ubuntu 22.04 [#2217](https://github.com/buildkite/agent/pull/2217) (@moskyb)

And the usual amount of @dependabot[bot] updates!

## [v3.49.0](https://github.com/buildkite/agent/tree/v3.49.0) (2023-06-21)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.48.0...v3.49.0)

### Fixed
- CreateArtifacts & UpdateArtifacts: remove sometimes-too-short timeout after 4 attempts [#2159](https://github.com/buildkite/agent/pull/2159) (@pda)
- Fix submodule mirror repository remote using main repo URL [#1998](https://github.com/buildkite/agent/pull/1998) (@francoiscampbell)
- Update job log file to include line transforms [#2157](https://github.com/buildkite/agent/pull/2157) (@chasestarr)
- Clearer HTTP error logging from API client [#2156](https://github.com/buildkite/agent/pull/2156) (@moskyb)

### Changed
- `Buildkite-Timeout-Milliseconds` API request header [#2160](https://github.com/buildkite/agent/pull/2160) (@pda)
- Extract pipeline parser to package internal/pipeline [#2158](https://github.com/buildkite/agent/pull/2158) (@DrJosh9000)
- Minor dependency updates [#2165](https://github.com/buildkite/agent/pull/2165) [#2164](https://github.com/buildkite/agent/pull/2164) [#2162](https://github.com/buildkite/agent/pull/2162) [#2161](https://github.com/buildkite/agent/pull/2161) [#2153](https://github.com/buildkite/agent/pull/2153) [#2152](https://github.com/buildkite/agent/pull/2152) [#2151](https://github.com/buildkite/agent/pull/2151)
- Lock library [#2145](https://github.com/buildkite/agent/pull/2145) (@DrJosh9000)


## [v3.48.0](https://github.com/buildkite/agent/tree/v3.48.0) (2023-06-06)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.47.0...v3.48.0)

The de-experimentification release!

- The `ansi-timestamps` experiment is now enabled by default. To remove the
  timestamps from your logs, you can pass the `--no-ansi-timestamps` flag.
- The `flock-file-locks` experiment is now enabled by default. Because the old
  and new file lock systems don't interact, we *strongly* recommend not running
  multiple agents of different versions on the same host.
- The `inbuilt-status-page` experiment is now enabled by default. For those
  running the agent with `--health-check-addr`, go to `/status` to see a
  human-friendly status page.

And whatever happened to `git-mirrors`? It graduated from experiment-hood in
v3.47.0!

### Changed
- De-experimentify ansi-timestamps [#2133](https://github.com/buildkite/agent/pull/2133) (@DrJosh9000)
- Preserve plugin config env var names with consecutive underscores [#2116](https://github.com/buildkite/agent/pull/2116) (@triarius)
- De-experimentify flock-file-locks [#2131](https://github.com/buildkite/agent/pull/2131) (@DrJosh9000)
- Report more AWS metadata [#2118](https://github.com/buildkite/agent/pull/2118) (@david-poirier)
- De-experimentify inbuilt-status-page [#2126](https://github.com/buildkite/agent/pull/2126) (@DrJosh9000)

### Fixed
- Fix origin for mirrored submodules [#2144](https://github.com/buildkite/agent/pull/2144) (@DrJosh9000)
- Wipe checkout directory on `git checkout` and `git fetch` failure and retry [#2137](https://github.com/buildkite/agent/pull/2137) (@triarius)


## [v3.47.0](https://github.com/buildkite/agent/tree/v3.47.0) (2023-05-25)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.46.1...v3.47.0)

Two new and very noteworthy experiments!

1. Have you ever wanted to write hooks in a compiled language? Or in Python or
   Ruby? Well now you can! With `--experiment=polyglot-hooks` the agent can run
   all sorts of hooks and plugins directly. Combined with
   `--experiment=job-api`, your hooks-of-a-different-language can alter
    environment variables through the local Job API!
2. Concurrency groups are great, but have you ever wanted to manage multiple
   agents running on the same host concurrently accessing a shared resource?
   Well now you can! With `--experiment=agent-api`, the agent now has an inbuilt
   locking service, accessible through new `lock` subcommands and also via a
   Unix socket (like the `job-api`).

### Added
- Experiment: Polyglot hooks [#2040](https://github.com/buildkite/agent/pull/2040) (@moskyb)
- Experiment: Local Agent API, with locking service [#2042](https://github.com/buildkite/agent/pull/2042) (@DrJosh9000)
- New flag `--upload-skip-symlinks` (on `artifact upload`) allows skipping symlinks when uploading files. `--follow-symlinks` has been deprecated and renamed to `--glob-resolve-follow-symlinks` [#2072](https://github.com/buildkite/agent/pull/2072) (@triarius)

### Fixed
- The `normalised-upload-paths` experiment was unintentionally left out of the available experiments list [#2076](https://github.com/buildkite/agent/pull/2076) (@MatthewDolan)

### Changed
- The `git-mirrors` experiment is promoted to full functionality [#2032](https://github.com/buildkite/agent/pull/2032) (@moskyb)
- Errors in the git checkout process are now easier to diagnose [#2074](https://github.com/buildkite/agent/pull/2074) (@moskyb)
- Alpine images updated to Alpine 3.18 [#2098](https://github.com/buildkite/agent/pull/2098) (@moskyb)

## [3.46.1](https://github.com/buildkite/agent/tree/3.46.1) (2023-05-08)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.46.0...v3.46.1)

### Fixed

- Avoid long `--no-patch` arg added to `git show` in v1.8.4, to e.g. support CentOS 7 [#2075](https://github.com/buildkite/agent/pull/2075) (@pda)

## [3.46.0](https://github.com/buildkite/agent/tree/3.46.0) (2023-05-04)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.45.0...v3.46.0)

### Added
- Add `*_PRIVATE_KEY` to auto-redacted vars [#2043](https://github.com/buildkite/agent/pull/2043) (@moskyb)
- Warn on unknown experiments [#2030](https://github.com/buildkite/agent/pull/2030) (@moskyb)
- More aws tags [#1994](https://github.com/buildkite/agent/pull/1994) (@sj26)
- Add option for outputting structured logs for collection and searching [#2009](https://github.com/buildkite/agent/pull/2009) (@goodspark)
- Include abbrev-commit in `buildkite:git:commit` meta-data [#2054](https://github.com/buildkite/agent/pull/2054) (@pda)
- Add agent support for getting meta-data by build [#2025](https://github.com/buildkite/agent/pull/2025) (@123sarahj123)

### Fixed
- Prevent job cancellation during checkout from retrying [#2047](https://github.com/buildkite/agent/pull/2047) [#2068](https://github.com/buildkite/agent/pull/2068) (@matthewborden + @triarius + @moskyb)
- ArtifactUploader API calls: faster timeout & retry [#2028](https://github.com/buildkite/agent/pull/2028) [#2069](https://github.com/buildkite/agent/pull/2069) (@pda)
- Give a nicer error when empty strings are used as metadata values [#2067](https://github.com/buildkite/agent/pull/2067) (@moskyb)
- Fix BUILDKITE_GIT_CLONE_MIRROR_FLAGS environment variable not working correctly [#2056](https://github.com/buildkite/agent/pull/2056) (@ppatwf)

As always, @dependabot and friends have been deep in the update mines ensuring that all of our dependencies are up to date. Thanks, dependabot!

## [3.45.0](https://github.com/buildkite/agent/tree/3.45.0) (2023-03-16)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.44.0...3.45.0)

It's a busy one! The major new feature in this release is the `job-api` experiment, which enables an HTTP API within the agent that allows jobs to inspect and mutate their environment, without using the normal bash-isms that we normally require. This is a big step towards supporting hooks and plugins in other languages, and we're really excited to see what you all do with it!

When this experiment is enabled, the agent will start an HTTP server on a unix domain socket, the address of which will be made available through the `BUILDKITE_AGENT_JOB_API_SOCKET` environment variable, with a token available through the `BUILDKITE_AGENT_JOB_API_TOKEN` environment variable. This socket can be used with the `buildkite-agent env {get,set,unset}` commands on the commandline, or directly through cURL or other HTTP client. Included in this release of the agent is a [golang client](https://github.com/buildkite/agent/blob/main/jobapi/client.go), which can be imported directly into your Go projects.

Also included is another experimental feature, `descending-spawn-priority`, which makes agents using the `--spawn-with-priority` flag spawn agents with a descending priority, rather than the default ascending priority. This is useful when running agents on heterogeneous hardware (ie, having two agents on one machine and four on another), as it means that jobs will be spread more evenly across the agents. For more information, see [the original issue](https://github.com/buildkite/agent/issues/1929), and [@DrJosh9000's PR](https://github.com/buildkite/agent/pull/2004). Huge thanks to @nick-f for bringing this to our attention!

Full changelog follows:

### Added
- Add current-job api [#1943](https://github.com/buildkite/agent/pull/1943) [#1944](https://github.com/buildkite/agent/pull/1944) [#2013](https://github.com/buildkite/agent/pull/2013) [#2017](https://github.com/buildkite/agent/pull/2017) (@moskyb + @DrJosh9000)
- Agent docker images now include [`buildx`](https://github.com/docker/buildx) [#2005](https://github.com/buildkite/agent/pull/2005) (@triarius)
- Add `descending-spawn-priority` experiment. [#2004](https://github.com/buildkite/agent/pull/2004) (@DrJosh9000)
- We now publish OSS acknowledgements with the agent. You can read them at [ACKNOWLEDGEMENTS.md](https://github.com/buildkite/agent/blob/main/ACKNOWLEDGEMENTS.md), or by running `buildkite-agent acknowledgements` [#1945](https://github.com/buildkite/agent/pull/1945) [#2000](https://github.com/buildkite/agent/pull/2000) (@DrJosh9000)
- BUILDKITE_S3_ENDPOINT env var, allowing jobs to upload artifacts to non-S3 endpoints eg minio [#1965](https://github.com/buildkite/agent/pull/1965) (@pda)

### Fixed
- Avoid holding full job logs, reducing agent memory consumption [#2014](https://github.com/buildkite/agent/pull/2014) (@DrJosh9000)
- ansi-timestamps: Compute prefixes at start of line [#2016](https://github.com/buildkite/agent/pull/2016) (@DrJosh9000)
- Fix DD trace setup warning [#2007](https://github.com/buildkite/agent/pull/2007) (@goodspark)

### Changed
- Kubernetes improvements:
  - Set a non-zero exit status when a job is cancelled in Kubernetes [#2010](https://github.com/buildkite/agent/pull/2010) (@triarius)
  - Add tags from env variables provided by the controller in agent-stack-k8s if kuberenetes-exec experiment is enabled [#2003](https://github.com/buildkite/agent/pull/2003) (@triarius)
- Globs parsed by the agent now support negation and bracketing [#2001](https://github.com/buildkite/agent/pull/2001) (@moskyb)
- Allow the use of non-bash shells to execute agent hooks [#1995](https://github.com/buildkite/agent/pull/1995) (@DrJosh9000)
- Don't add custom remotes for submodules when using git-mirrors [#1991](https://github.com/buildkite/agent/pull/1991) (@jonahbull)
- Improve systemd behaviour when updating the agent [#1993](https://github.com/buildkite/agent/pull/1993) (@triarius)
- ... And as always, the usual crop of small fixes, dependency updates, and cleanups (@moskyb, @dependabot, @DrJosh9000, @triarius)

## [v3.44.0](https://github.com/buildkite/agent/tree/v3.44.0) (2023-02-27)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.43.1...v3.44.0)

### Fixed
- tini is once again available at the old path (`/usr/sbin/tini`) in the Ubuntu 20.04 image [#1934](https://github.com/buildkite/agent/pull/1934) (@triarius)
- With `ansi-timestamps` experiment enabled, each line's timestamp is now computed at the end of the line [#1940](https://github.com/buildkite/agent/pull/1940) (@DrJosh9000)
- A panic when the AWS region for an S3 bucket is undiscoverable [#1964](https://github.com/buildkite/agent/pull/1964) (@DrJosh9000)

### Added
- An experiment for running jobs under Kubernetes [#1884](https://github.com/buildkite/agent/pull/1884) (@benmoss), [#1968](https://github.com/buildkite/agent/pull/1968) (@triarius)
- Ubuntu 22.04 Docker Image [#1966](https://github.com/buildkite/agent/pull/1966) (@triarius)
- Claims can now be added to OIDC token requests [#1951](https://github.com/buildkite/agent/pull/1951) (@triarius)
- A new flag / environment variable (`--git-checkout-flags` / `BUILDKITE_GIT_CHECKOUT_FLAGS`) for passing extra flags to `git checkout` [#1891](https://github.com/buildkite/agent/pull/1891) (@jmelahman)
- Reference clones can be used for submodules [#1959](https://github.com/buildkite/agent/pull/1959) (@jonahbull)

### Changed
- Upstart is no longer supported [#1946](https://github.com/buildkite/agent/pull/1946) (@sj26)
- `pipeline upload` internally uses a new asynchronous upload flow, reducing the number of connections held open [#1927](https://github.com/buildkite/agent/pull/1927) (@triarius)
- Faster failure when trying to `pipeline upload` a malformed pipeline [#1963](https://github.com/buildkite/agent/pull/1963) (@triarius)
- Better errors when config loading fails [#1937](https://github.com/buildkite/agent/pull/1937) (@moskyb)
- Pipelines are now parsed with gopkg.in/yaml.v3. This change should be invisible, but involved a non-trivial amount of new code. [#1930](https://github.com/buildkite/agent/pull/1930) (@DrJosh9000)
- Many dependency updates, notably Go v1.20.1 [#1955](https://github.com/buildkite/agent/pull/1955).
- Several minor fixes, improvements and clean-ups (@sj26, @triarius, @jonahbull, @DrJosh9000, @tcptps, @dependabot[bot])

## [3.43.1](https://github.com/buildkite/agent/tree/3.43.1) (2023-01-20)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.43.0...3.43.1)

### Fixed
- An issue introduced in v3.43.0 where agents running in acquire mode would exit after ~4.5 minutes, failing the job they were running [#1923](https://github.com/buildkite/agent/pull/1923) (@leathekd)

## [3.43.0](https://github.com/buildkite/agent/tree/3.43.0) (2023-01-18)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.42.0...3.43.0)

### Fixed
- A nil pointer dereference introduced in 3.42.0 due to missing error handling after calling `user.Current` [#1910](https://github.com/buildkite/agent/pull/1910) (@DrJosh9000)

### Added
- A flag to allow empty results with doing an artifact search [#1887](https://github.com/buildkite/agent/pull/1887) (@MatthewDolan)
- Docker Images for linux/arm64 [#1901](https://github.com/buildkite/agent/pull/1901) (@triarius)
- Agent tags are added from ECS container metadata [#1870](https://github.com/buildkite/agent/pull/1870) (@francoiscampbell)

### Changed
- The `env` subcommand is now `env dump` [#1920](https://github.com/buildkite/agent/pull/1920) (@pda)
- AcquireJob now retries while the job is locked [#1894](https://github.com/buildkite/agent/pull/1894) (@triarius)
- Various miscellaneous updates and improvements (@moskyb, @triarius, @mitchbne, @dependabot[bot])

## [v3.42.0](https://github.com/buildkite/agent/tree/v3.42.0) (2023-01-05)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.41.0...v3.42.0)

 ### Added
 - Add an in-built hierarchical status page [#1873](https://github.com/buildkite/agent/pull/1873) (@DrJosh9000)
 - Add an `agent-startup` hook that fires at the same time as the `agent-shutdown` hook is registered [#1778](https://github.com/buildkite/agent/pull/1778) (@donalmacc)

 ### Changed
- Enforce a timeout on `finishJob` and `onUploadChunk` [#1854](https://github.com/buildkite/agent/pull/1854) (@DrJosh9000)
- A variety of dependency updates, documentation, and code cleanups! (@dependabot[bot], @DrJosh9000, @moskyb)
- Flakey test fixes and test suite enhancements (@triarius, @DrJosh9000)

 ### Fixed
 - Ensure that unrecoverable errors for Heartbeat and Ping stop the agent [#1855](https://github.com/buildkite/agent/pull/1855) (@moskyb)

 ### Security
 - Update `x/crypto/ssh` to `0.3.0`, patching CVE-2020-9283 [#1857](https://github.com/buildkite/agent/pull/1857) (@moskyb)


## [v3.41.0](https://github.com/buildkite/agent/tree/v3.41.0) (2022-11-24)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.40.0...v3.41.0)

### Added
- Experimental `buildkite-agent oidc request-token` command [#1827](https://github.com/buildkite/agent/pull/1827) (@triarius)
- Option to set the service name for tracing [#1779](https://github.com/buildkite/agent/pull/1779) (@goodspark)

### Changed

- Update windows install script to detect arm64 systems [#1768](https://github.com/buildkite/agent/pull/1768) (@yob)
- Install docker compose v2 plugin in agent alpine and ubuntu docker images [#1841](https://github.com/buildkite/agent/pull/1841) (@ajoneil) (@triarius)
- 🧹 A variety of dependency updates, documentation, and cleanups!  (@DrJosh9000)


## [v3.40.0](https://github.com/buildkite/agent/tree/v3.40.0) (2022-11-08)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.39.0...v3.40.0)

### Added

- Agent binaries for windows/arm64 [#1767](https://github.com/buildkite/agent/pull/1767) (@yob)
- Alpine k8s image [#1771](https://github.com/buildkite/agent/pull/1771) (@dabarrell)

### Security

- (Fixed in 3.39.1) A security issue in environment handling between buildkite-agent and Bash 5.2 [#1781](https://github.com/buildkite/agent/pull/1781) (@moskyb)
- Secret redaction now handles secrets containing UTF-8 code points greater than 255 [#1809](https://github.com/buildkite/agent/pull/1809) (@DrJosh9000)
- The update to Go 1.19.3 fixes two Go security issues (particularly on Windows):
   - The current directory (`.`) in `$PATH` is now ignored for finding executables - see https://go.dev/blog/path-security
   - Environment variable values containing null bytes are now sanitised - see https://github.com/golang/go/issues/56284

### Changed

- 5xx responses are now retried when attempting to start a job [#1777](https://github.com/buildkite/agent/pull/1777) (@jonahbull)
- 🧹 A variety of dependency updates and cleanups!

## [v3.39.0](https://github.com/buildkite/agent/tree/v3.39.0) (2022-09-08)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.38.0...v3.39.0)

### Added
- gcp:instance-name and tweak GCP labels fetching [#1742](https://github.com/buildkite/agent/pull/1742) (@pda)
- Support for not-yet-released per-job agent tokens [#1745](https://github.com/buildkite/agent/pull/1745) (@moskyb)

### Changed
- Retry Disconnect API calls [#1761](https://github.com/buildkite/agent/pull/1761) (@pda)
- Only search for finished artifacts [#1728](https://github.com/buildkite/agent/pull/1728) (@moskyb)
- Cache S3 clients between artifact downloads [#1732](https://github.com/buildkite/agent/pull/1732) (@moskyb)
- Document label edge case [#1718](https://github.com/buildkite/agent/pull/1718) (@plaindocs)

### Fixed
- Docker: run /sbin/tini without -g for graceful termination [#1763](https://github.com/buildkite/agent/pull/1763) (@pda)
- Fix multiple-nested plugin repos on gitlab [#1746](https://github.com/buildkite/agent/pull/1746) (@moskyb)
- Fix unowned plugin reference [#1733](https://github.com/buildkite/agent/pull/1733) (@moskyb)
- Fix order of level names for logger.Level.String() [#1722](https://github.com/buildkite/agent/pull/1722) (@moskyb)
- Fix warning log level [#1721](https://github.com/buildkite/agent/pull/1721) (@ChrisBr)

## [v3.38.0](https://github.com/buildkite/agent/tree/v3.38.0) (2022-07-20)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.37.0...v3.38.0)

### Changed
- Include a list of enabled features in the register request [#1706](https://github.com/buildkite/agent/pull/1706) (@moskyb)
- Promote opentelemetry tracing to mainline feature status [#1702](https://github.com/buildkite/agent/pull/1702) (@moskyb)
- Improve opentelemetry implementation [#1699](https://github.com/buildkite/agent/pull/1699) [#1705](https://github.com/buildkite/agent/pull/1705) (@moskyb)

## [v3.37.0](https://github.com/buildkite/tree/v3.37.0) (2022-07-06)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.36.1...v3.37.0)

### Added

* Agent metadata includes `arch` (e.g. `arch=amd64`) alongside `hostname` and `os` [#1691](https://github.com/buildkite/agent/pull/1691) ([moskyb](https://github.com/moskyb))
* Allow forcing clean checkout of plugins [#1636](https://github.com/buildkite/agent/pull/1636) ([toothbrush](https://github.com/toothbrush))

### Fixed

* Environment modification in hooks that set bash arrays [#1692](https://github.com/buildkite/agent/pull/1692) ([moskyb](https://github.com/moskyb))
* Unescape backticks when parsing env from export -p output [#1687](https://github.com/buildkite/agent/pull/1687) ([moskyb](https://github.com/moskyb))
* Log “Using flock-file-locks experiment 🧪” when enabled [#1688](https://github.com/buildkite/agent/pull/1688) ([lox](https://github.com/lox))
* flock-file-locks experiment: errors logging [#1689](https://github.com/buildkite/agent/pull/1689) ([KevinGreen](https://github.com/KevinGreen))
* Remove potentially-corrupted mirror dir if clone fails [#1671](https://github.com/buildkite/agent/pull/1671) ([lox](https://github.com/lox))
* Improve log-level flag usage description [#1676](https://github.com/buildkite/agent/pull/1676) ([pzeballos](https://github.com/pzeballos))

### Changed

* datadog-go major version upgrade to v5.1.1 [#1666](https://github.com/buildkite/agent/pull/1666) ([moskyb](https://github.com/moskyb))
* Revert to delegating directory creation permissions to system umask [#1667](https://github.com/buildkite/agent/pull/1667) ([moskyb](https://github.com/moskyb))
* Replace retry code with [roko](https://github.com/buildkite/roko) [#1675](https://github.com/buildkite/agent/pull/1675) ([moskyb](https://github.com/moskyb))
* bootstrap/shell: round command durations to 5 significant digits [#1651](https://github.com/buildkite/agent/pull/1651) ([kevinburkesegment](https://github.com/kevinburkesegment))


## [v3.36.1](https://github.com/buildkite/agent/tree/v3.36.1) (2022-05-27)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.36.0...v3.36.1)

### Fixed
- Fix nil pointer deref when using --log-format json [#1653](https://github.com/buildkite/agent/pull/1653) (@moskyb)

## [v3.36.0](https://github.com/buildkite/agent/tree/v3.36.0) (2022-05-17)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.35.2...v3.36.0)

### Added

- Add experiment to use kernel-based flocks instead of lockfiles [#1624](https://github.com/buildkite/agent/pull/1624) (@KevinGreen)
- Add option to enable temporary job log file [#1564](https://github.com/buildkite/agent/pull/1564) (@albertywu)
- Add experimental OpenTelemetry Tracing Support [#1631](https://github.com/buildkite/agent/pull/1631) + [#1632](https://github.com/buildkite/agent/pull/1632) (@moskyb)
- Add `--log-level` flag to all commands [#1635](https://github.com/buildkite/agent/pull/1635) (@moskyb)

### Fixed

- The `no-plugins` option now works correctly when set in the config file [#1579](https://github.com/buildkite/agent/pull/1579) (@elruwen)
- Clear up usage instructions around `--disconnect-after-idle-timeout` and `--disconnect-after-job` [#1599](https://github.com/buildkite/agent/pull/1599) (@moskyb)

### Changed
- Refactor retry machinery to allow the use of exponential backoff [#1588](https://github.com/buildkite/agent/pull/1588) (@moskyb)
- Create all directories with 0775 permissions [#1616](https://github.com/buildkite/agent/pull/1616) (@moskyb)
- Dependency Updates:
  - github.com/urfave/cli: 1.22.4 -> 1.22.9 [#1619](https://github.com/buildkite/agent/pull/1619) + [#1638](https://github.com/buildkite/agent/pull/1638)
  - Golang: 1.17.6 -> 1.18.1 (yay, generics!) [#1603](https://github.com/buildkite/agent/pull/1603) + [#1627](https://github.com/buildkite/agent/pull/1627)
  - Alpine Build Images: 3.15.0 -> 3.15.4 [#1626](https://github.com/buildkite/agent/pull/1626)
  - Alpine Release Images: 3.12 -> 3.15.4 [#1628](https://github.com/buildkite/agent/pull/1628) (@moskyb)

## [v3.35.2](https://github.com/buildkite/agent/tree/v3.35.2) (2022-04-13)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.35.1...v3.35.2)

### Fixed
- Fix race condition in bootstrap.go [#1606](https://github.com/buildkite/agent/pull/1606) (@moskyb)

### Changed
- Bump some dependency versions - thanks @dependabot!
  - github.com/stretchr/testify: 1.5.1 -> 1.7.1 [#1608](https://github.com/buildkite/agent/pull/1608)
  - github.com/mitchellh/go-homedir: 1.0.0 -> 1.1.0 [#1576](https://github.com/buildkite/agent/pull/1576)

## [v3.35.1](https://github.com/buildkite/agent/tree/v3.35.1) (2022-04-05)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.35.0...v3.35.1)

### Fixed

- Revert file permission changes made in [#1580](https://github.com/buildkite/agent/pull/1580). They were creating issues with docker-based workflows [#1601](https://github.com/buildkite/agent/pull/1601) (@pda + @moskyb)

## [v3.35.0](https://github.com/buildkite/agent/tree/v3.35.0) (2022-03-23)
[Full Changelog](https://github.com/buildkite/agent/compare/v3.34.0...v3.35.0)

### Changed

- Make `go fmt` mandatory in this repo [#1587](https://github.com/buildkite/agent/pull/1587) (@moskyb)
- Only search for finished artifact uploads when using `buildkite-agent artifact download` and `artifact shasum` [#1584](https://github.com/buildkite/agent/pull/1584) (@pda)
- Improve help/usage/errors for `buildkite-agent artifact shasum` [#1581](https://github.com/buildkite/agent/pull/1581) (@pda)
- Make the agent look for work immediately after completing a job, rather than waiting the ping interval [#1567](https://github.com/buildkite/agent/pull/1567) (@extemporalgenome)
- Update github.com/aws/aws-sdk-go to the latest v1 release [#1573](https://github.com/buildkite/agent/pull/1573) (@yob)
- Enable dependabot for go.mod [#1574](https://github.com/buildkite/agent/pull/1574) (@yob)
- Use build matrix feature to simplify CI pipeline [#1566](https://github.com/buildkite/agent/pull/1566) (@ticky)
  - Interested in using Build Matrices yourself? Check out [our docs!](https://buildkite.com/docs/pipelines/build-matrix)
- Buildkite pipeline adjustments [#1597](https://github.com/buildkite/agent/pull/1597) (@moskyb)

### Fixed

- Use `net.JoinHostPort()` to join host/port combos, rather than `fmt.Sprintf()` [#1585](https://github.com/buildkite/agent/pull/1585) (@pda)
- Fix minor typo in help text for `buildkite-agent pipeline upload [#1595](https://github.com/buildkite/agent/pull/1595) (@moskyb)

### Added

- Add option to skip updating the mirror when using git mirrors. Useful when git is mounted from an external volume, NFS mount etc [#1552](https://github.com/buildkite/agent/pull/1552) (@fatmcgav)
- Use the more secure SHA256 hashing algorithm alongside SHA1 when working with artifacts [#1582](https://github.com/buildkite/agent/pull/1582) [#1583](https://github.com/buildkite/agent/pull/1583) [#1584](https://github.com/buildkite/agent/pull/1584) (@pda)

### Security

- When creating directories, make them only accessible by current user and group [#1580](https://github.com/buildkite/agent/pull/1580) (@pda)

## [v3.34.1](https://github.com/buildkite/agent/compare/v3.34.0...v3.34.1) (2022-03-23)

### Fixed

- Make secret value rejection on pipeline upload optional. **This undoes a breaking change accidentally included in v3.34.0** [#1589](https://github.com/buildkite/agent/pull/1589) (@moskyb)

## [v3.34.0](https://github.com/buildkite/agent/compare/v3.33.3...v3.34.0) (2022-03-01)

### Added

* Introduce `spawn-with-priority` option [#1530](https://github.com/buildkite/agent/pull/1530) ([sema](https://github.com/sema))

### Fixed

* Retry 500 responses when batch creating artifacts [#1568](https://github.com/buildkite/agent/pull/1568) ([moskyb](https://github.com/moskyb))
* Report OS versions when running on AIX and Solaris [#1559](https://github.com/buildkite/agent/pull/1559) ([yob](https://github.com/yob))
* Support multiple commands on Windows [#1543](https://github.com/buildkite/agent/pull/1543) ([keithduncan](https://github.com/keithduncan))
* Allow `BUILDKITE_S3_DEFAULT_REGION` to be used for unconditional bucket region [#1535](https://github.com/buildkite/agent/pull/1535) ([keithduncan](https://github.com/keithduncan))

### Changed

* Go version upgraded from 1.16 to 1.17 [#1557](https://github.com/buildkite/agent/pull/1557) [#1549](https://github.com/buildkite/agent/pull/1549)
* Remove the CentOS (end-of-life) docker image [#1561](https://github.com/buildkite/agent/pull/1561) ([tessereth](https://github.com/tessereth))
* Plugin `git clone` is retried up to 3 times [#1539](https://github.com/buildkite/agent/pull/1539) ([pzeballos](https://github.com/pzeballos))
* Docker image alpine upgraded from 3.14.2 to 3.15.0 [#1541](https://github.com/buildkite/agent/pull/1541)

### Security

* Lock down file permissions on windows [#1562](https://github.com/buildkite/agent/pull/1562) ([tessereth](https://github.com/tessereth))
* Reject pipeline uploads containing redacted vars [#1523](https://github.com/buildkite/agent/pull/1523) ([keithduncan](https://github.com/keithduncan))

## [v3.33.3](https://github.com/buildkite/agent/compare/v3.33.2...v3.33.3) (2021-09-29)

### Fixed

* Fix erroneous working directory change for hooks that early exit [#1520](https://github.com/buildkite/agent/pull/1520)

## [v3.33.2](https://github.com/buildkite/agent/compare/v3.33.1...v3.33.2) (2021-09-29)

### Fixed

* Non backwards compatible change to artifact download path handling [#1518](https://github.com/buildkite/agent/pull/1518)

## [v3.33.1](https://github.com/buildkite/agent/compare/v3.33.0...v3.33.1) (2021-09-28)

### Fixed

* A crash in `buildkite-agent bootstrap` when command hooks early exit [#1516](https://github.com/buildkite/agent/pull/1516)

## [v3.33.0](https://github.com/buildkite/agent/compare/v3.32.3...v3.33.0) (2021-09-27)

### Added

* Support for `unset` environment variables in Job Lifecycle Hooks [#1488](https://github.com/buildkite/agent/pull/1488)

### Changed

* Remove retry handling when deleting annotations that are already deleted [#1507](https://github.com/buildkite/agent/pull/1507) ([@lox](https://github.com/lox))
* Alpine base image from 3.14.0 to 3.14.2 [#1499](https://github.com/buildkite/agent/pull/1499)

### Fixed

* Support for trailing slash path behaviour in artifact download [#1504](https://github.com/buildkite/agent/pull/1504) ([@jonathan-brand](https://github.com/jonathan-brand))

## [v3.32.3](https://github.com/buildkite/agent/compare/v3.32.2...v3.32.3) (2021-09-01)

### Fixed

* PowerShell hooks on Windows [#1497](https://github.com/buildkite/agent/pull/1497)

## [v3.32.2](https://github.com/buildkite/agent/compare/v3.32.1...v3.32.2) (2021-08-31)

### Added

* Improved error logging around AWS Credentials [#1490](https://github.com/buildkite/agent/pull/1490)
* Logging to the artifact upload command to say where artifacts are being sent [#1486](https://github.com/buildkite/agent/pull/1486)
* Support for cross-region artifact buckets [#1495](https://github.com/buildkite/agent/pull/1495)

### Changed

* artifact_paths failures no longer mask a command error [#1487](https://github.com/buildkite/agent/pull/1487)

### Fixed

* Failed plug-in checkouts using the default branch instead of the requested version [#1493](https://github.com/buildkite/agent/pull/1493)
* Missing quote in the PowerShell hook wrapper [#1494](https://github.com/buildkite/agent/pull/1494)

## [v3.32.1](https://github.com/buildkite/agent/compare/v3.32.0...v3.32.1) (2021-08-06)

### Fixed

* A panic in the log redactor when processing certain bytes [#1478](https://github.com/buildkite/agent/issues/1478) ([scv119](https://github.com/scv119))

## [v3.32.0](https://github.com/buildkite/agent/compare/v3.31.0...v3.32.0) (2021-07-30)

### Added

* A new pre-bootstrap hook which can accept or reject jobs before environment variables are loaded [#1456](https://github.com/buildkite/agent/pull/1456)
* `ppc64` and `ppc64le` architecture binaries to the DEB and RPM packages [#1474](https://github.com/buildkite/agent/pull/1474) [#1473](https://github.com/buildkite/agent/pull/1473) ([staticfloat](https://github.com/staticfloat))
* Use text/yaml mime type for .yml and .yaml artifacts [#1470](https://github.com/buildkite/agent/pull/1470)

### Changed

* Add BUILDKITE_BIN_PATH to end, not start, of PATH [#1465](https://github.com/buildkite/agent/pull/1465) ([DavidSpickett](https://github.com/DavidSpickett))

## [v3.31.0](https://github.com/buildkite/agent/compare/v3.30.0...v3.31.0) (2021-07-02)

### Added

* Output secret redaction is now on by default [#1452](https://github.com/buildkite/agent/pull/1452)
* Improved CLI docs for `buildkite-agent artifact download` [#1446](https://github.com/buildkite/agent/pull/1446)

### Changed

* Build using golang 1.16.5 [#1460](https://github.com/buildkite/agent/pull/1460)

### Fixed

* Discovery of the `buildkite-agent` binary path in more situations [#1444](https://github.com/buildkite/agent/pull/1444) [#1457](https://github.com/buildkite/agent/pull/1457)

## [v3.30.0](https://github.com/buildkite/agent/compare/v3.29.0...v3.30.0) (2021-05-28)

### Added
* Send queue metrics to Datadog when job received [#1442](https://github.com/buildkite/agent/pull/1442) ([keithduncan](https://github.com/keithduncan))
* Add flag to send Datadog Metrics as Distributions [#1433](https://github.com/buildkite/agent/pull/1433) ([amukherjeetwilio](https://github.com/amukherjeetwilio))
* Ubuntu 18.04 based Docker image [#1441](https://github.com/buildkite/agent/pull/1441) ([keithduncan](https://github.com/keithduncan))
* Build binaries for `netbsd` and `s390x` [#1432](https://github.com/buildkite/agent/pull/1432), [#1421](https://github.com/buildkite/agent/pull/1421) ([yob](https://github.com/yob))
* Add `wait-for-ec2-meta-data-timeout` config variable [#1425](https://github.com/buildkite/agent/pull/1425) ([OliverKoo](https://github.com/OliverKoo))

### Changed
* Build using golang 1.16.4 [#1429](https://github.com/buildkite/agent/pull/1429)
* Replace kr/pty with creack/pty and upgrade from 1.1.2 to 1.1.12 [#1431](https://github.com/buildkite/agent/pull/1431) ([ibuclaw](https://github.com/ibuclaw))

### Fixed
* Trim trailing slash from `buildkite-agent artifact upload` when using custom S3 bucket paths [#1427](https://github.com/buildkite/agent/pull/1427) ([shevaun](https://github.com/shevaun))
* Use /usr/pkg/bin/bash as default shell on NetBSD [#1430](https://github.com/buildkite/agent/pull/1430) ([ibuclaw](https://github.com/ibuclaw))

## [v3.29.0](https://github.com/buildkite/agent/compare/v3.28.1...v3.29.0) (2021-04-21)

### Changed
* Support mips64le architecture target. [#1379](https://github.com/buildkite/agent/pull/1379) ([houfangdong](https://github.com/houfangdong))
* Search the path for bash when running bootstrap scripts [#1404](https://github.com/buildkite/agent/pull/1404) ([yob](https://github.com/yob))
* Output-redactor: redact shell logger, including changed env vars [#1401](https://github.com/buildkite/agent/pull/1401) ([pda](https://github.com/pda))
* Add *_ACCESS_KEY & *_SECRET_KEY to default redactor-var [#1405](https://github.com/buildkite/agent/pull/1405) ([pda](https://github.com/pda))
* Build with Golang 1.16.3 [#1412](https://github.com/buildkite/agent/pull/1412) ([dependabot[bot]](https://github.com/apps/dependabot))
* Update [Buildkite CLI](https://github.com/buildkite/cli) release from 1.0.0 to 1.2.0 [#1403](https://github.com/buildkite/agent/pull/1403) ([yob](https://github.com/yob))

### Fixed
* Avoid occasional failure to run jobs when working directory is missing [#1402](https://github.com/buildkite/agent/pull/1402) ([yob](https://github.com/yob))
* Avoid a rare panic when running `buildkite-agent pipeline upload` [#1406](https://github.com/buildkite/agent/pull/1406) ([yob](https://github.com/yob))

## [v3.28.1](https://github.com/buildkite/agent/compare/v3.27.0...v3.28.1)

### Added

* collect instance-life-cycle as a default tag on EC2 instances [#1374](https://github.com/buildkite/agent/pull/1374) [yob](https://github.com/yob))
* Expose plugin config in two new instance variables, `BUILDKITE_PLUGIN_NAME` and `BUILDKITE_PLUGIN_CONFIGURATION` [#1382](https://github.com/buildkite/agent/pull/1382) [moensch](https://github.com/moensch)
* Add `buildkite-agent annotation remove` command [#1364](https://github.com/buildkite/agent/pull/1364/) [ticky](https://github.com/ticky)
* Allow customizing the signal bootstrap sends to processes on cancel  [#1390](https://github.com/buildkite/agent/pull/1390/) [brentleyjones](https://github.com/brentleyjones)

### Changed

* On new installs the default agent name has changed from `%hostname-%n` to `%hostname-%spawn` [#1389](https://github.com/buildkite/agent/pull/1389) [pda](https://github.com/pda)

### Fixed

* Fixed --no-pty flag [#1394][https://github.com/buildkite/agent/pull/1394] [pda](https://github.com/pda)

## v3.28.0

* Skipped due to a versioning error

## [v3.27.0](https://github.com/buildkite/agent/compare/v3.26.0...v3.27.0)

### Added
* Add support for agent tracing using Datadog APM [#1273](https://github.com/buildkite/agent/pull/1273) ([goodspark](https://github.com/goodspark), [Sam Schlegel](https://github.com/samschlegel))
* Improvements to ARM64 support (Apple Silicon/M1) [#1346](https://github.com/buildkite/agent/pull/1346), [#1354](https://github.com/buildkite/agent/pull/1354), [#1343](https://github.com/buildkite/agent/pull/1343) ([ticky](https://github.com/ticky))
* Add a Linux ppc64 build to the pipeline [#1362](https://github.com/buildkite/agent/pull/1362) ([ticky](https://github.com/ticky))
* Agent can now upload artifacts using AssumedRoles using `BUILDKITE_S3_SESSION_TOKEN` [#1359](https://github.com/buildkite/agent/pull/1359) ([grahamc](https://github.com/grahamc))
* Agent name `%spawn` interpolation to deprecate/replace `%n` [#1377](https://github.com/buildkite/agent/pull/1377) ([ticky](https://github.com/ticky))

### Changed
* Compile the darwin/arm64 binary using go 1.16rc1 [#1352](https://github.com/buildkite/agent/pull/1352) ([yob](https://github.com/yob)) [#1369](https://github.com/buildkite/agent/pull/1369) ([chloeruka](https://github.com/chloeruka))
* Use Docker CLI packages, update Docker Compose, and update centos to 8.x [#1351](https://github.com/buildkite/agent/pull/1351) ([RemcodM](https://github.com/RemcodM))

## Fixed
* Fixed an issue in #1314 that broke pull requests with git-mirrors [#1347](https://github.com/buildkite/agent/pull/1347) ([ticky](https://github.com/ticky))

## [v3.26.0](https://github.com/buildkite/agent/compare/v3.25.0...v3.26.0) (2020-12-03)

### Added

* Compile an experimental native executable for Apple Silicon [#1339](https://github.com/buildkite/agent/pull/1339) ([yob](https://github.com/yob))
  * Using a pre-release version of go, we'll switch to compiling with go 1.16 once it's released

### Changed

* Install script: use the arm64 binary for aarch64 machines [#1340](https://github.com/buildkite/agent/pull/1340) ([gc-plp](https://github.com/gc-plp))
* Build with golang 1.15 [#1334](https://github.com/buildkite/agent/pull/1334) ([yob](https://github.com/yob))
* Bump alpine docker image from alpine 3.8 to 3.12 [#1333](https://github.com/buildkite/agent/pull/1333) ([yob](https://github.com/yob))
* Upgrade docker ubuntu to 20.04 focal [#1312](https://github.com/buildkite/agent/pull/1312) ([sj26](https://github.com/sj26))

## [v3.25.0](https://github.com/buildkite/agent/compare/v3.24.0...v3.25.0) (2020-10-21)

### Added
* Add --mirror flag by default for mirror clones [#1328](https://github.com/buildkite/agent/pull/1328) ([chrislloyd](https://github.com/chrislloyd))
* Add an agent-wide shutdown hook [#1275](https://github.com/buildkite/agent/pull/1275) ([goodspark](https://github.com/goodspark)) [#1322](https://github.com/buildkite/agent/pull/1322) ([pda](https://github.com/pda))

### Fixed
* Improve windows telemetry so that we report the version accurately in-platform [#1330](https://github.com/buildkite/agent/pull/1330) ([yob](https://github.com/yob)) [#1316](https://github.com/buildkite/agent/pull/1316) ([yob](https://github.com/yob))
* Ensure no orphaned processes when Windows jobs complete [#1329](https://github.com/buildkite/agent/pull/1329) ([yob](https://github.com/yob))
* Log error messages when canceling a running job fails [#1317](https://github.com/buildkite/agent/pull/1317) ([yob](https://github.com/yob))
* gitCheckout() validates branch, plus unit tests [#1315](https://github.com/buildkite/agent/pull/1315) ([pda](https://github.com/pda))
* gitFetch() terminates options with -- before repo/refspecs [#1314](https://github.com/buildkite/agent/pull/1314) ([pda](https://github.com/pda))

## [v3.24.0](https://github.com/buildkite/agent/compare/v3.23.1...v3.24.0) (2020-09-29)

### Fixed
* Fix build script [#1300](https://github.com/buildkite/agent/pull/1300) ([goodspark](https://github.com/goodspark))
* Fix typos [#1297](https://github.com/buildkite/agent/pull/1297) ([JuanitoFatas](https://github.com/JuanitoFatas))
* Fix flaky tests: experiments and parallel tests don't mix [#1295](https://github.com/buildkite/agent/pull/1295) ([pda](https://github.com/pda))
* artifact_uploader_test fixed for Windows. [#1288](https://github.com/buildkite/agent/pull/1288) ([pda](https://github.com/pda))
* Windows integration tests: git file path quirk fix [#1291](https://github.com/buildkite/agent/pull/1291) ([pda](https://github.com/pda))

### Changed
* git-mirrors: set BUILDKITE_REPO_MIRROR=/path/to/mirror/repo [#1311](https://github.com/buildkite/agent/pull/1311) ([pda](https://github.com/pda))
* Truncate BUILDKITE_MESSAGE to 64 KiB [#1307](https://github.com/buildkite/agent/pull/1307) ([pda](https://github.com/pda))
* CI: windows tests on queue=buildkite-agent-windows without Docker [#1294](https://github.com/buildkite/agent/pull/1294) ([pda](https://github.com/pda))
* buildkite:git:commit meta-data via stdin; avoid Argument List Too Long [#1302](https://github.com/buildkite/agent/pull/1302) ([pda](https://github.com/pda))
* Expand the CLI test step [#1293](https://github.com/buildkite/agent/pull/1293) ([ticky](https://github.com/ticky))
* Improve Apple Silicon Mac support [#1289](https://github.com/buildkite/agent/pull/1289) ([ticky](https://github.com/ticky))
* update github.com/urfave/cli to the latest v1 release [#1287](https://github.com/buildkite/agent/pull/1287) ([yob](https://github.com/yob))


## [v3.23.1](https://github.com/buildkite/agent/compare/v3.23.0...v3.23.1) (2020-09-09)

### Fixed
* Fix CLI flag/argument order sensitivity regression [#1286](https://github.com/buildkite/agent/pull/1286) ([yob](https://github.com/yob))


## [v3.23.0](https://github.com/buildkite/agent/compare/v3.22.1...v3.23.0) (2020-09-04)

### Added
* New artifact search subcommand [#1278](https://github.com/buildkite/agent/pull/1278) ([chloeruka](https://github.com/chloeruka))
![image](https://user-images.githubusercontent.com/30171259/92212159-e32bd700-eed4-11ea-9af8-2ad024eaecc1.png)
* Add sidecar agent suitable for being shared via volume in ECS or Kubernetes [#1218](https://github.com/buildkite/agent/pull/1218) ([keithduncan](https://github.com/keithduncan)) [#1263](https://github.com/buildkite/agent/pull/1263) ([yob](https://github.com/yob))
* We now fetch amd64 binaries on Apple Silicon Macs in anticipation of new macOS ARM computers [#1237](https://github.com/buildkite/agent/pull/1237) ([ticky](https://github.com/ticky))
* Opt-in experimental `resolve-commit-after-checkout` flag to resolve `BUILDKITE_COMMIT` refs (for example, "HEAD") to a commit hash [#1256](https://github.com/buildkite/agent/pull/1256) ([jayco](https://github.com/jayco))
* Experimental: Build & publish RPM ARM64 package for aarch64 [#1243](https://github.com/buildkite/agent/pull/1243) ([chloeruka](https://github.com/chloeruka)) [#1241](https://github.com/buildkite/agent/pull/1241) ([chloeruka](https://github.com/chloeruka))

### Changed
* Stop building i386 for darwin after 14 years of amd64 Mac hardware [#1238](https://github.com/buildkite/agent/pull/1238) ([pda](https://github.com/pda))
* Updated github.com/urfave/cli to v2 - this is the CLI framework we use for agent commands. [#1233](https://github.com/buildkite/agent/pull/1233) ([yob](https://github.com/yob)) [#1250](https://github.com/buildkite/agent/pull/1250) ([yob](https://github.com/yob))
* Send the reported system and machine names when fetching latest release [#1240](https://github.com/buildkite/agent/pull/1240) ([ticky](https://github.com/ticky))
* Bump build environment from [go](https://github.com/golang/go) 1.14.2 to 1.14.7 [#1235](https://github.com/buildkite/agent/pull/1235) ([yob](https://github.com/yob)) [#1262](https://github.com/buildkite/agent/pull/1262) ([yob](https://github.com/yob))
* Update [aws-sdk-go](https://github.com/aws/aws-sdk-go) to 1.32.10 [#1234](https://github.com/buildkite/agent/pull/1234) ([yob](https://github.com/yob))

### Fixed
* `git-mirrors` experiment now only fetches branch instead of a full remote update [#1112](https://github.com/buildkite/agent/pull/1112) ([lox](https://github.com/lox))
* Hooks can introduce empty environment variables [#1232](https://github.com/buildkite/agent/pull/1232) ([pda](https://github.com/pda))
* ArtifactUploader now deduplicates upload file paths [#1268](https://github.com/buildkite/agent/pull/1268) ([pda](https://github.com/pda))
* Added additional logging to artifact uploads  [#1266](https://github.com/buildkite/agent/pull/1266) ([yob](https://github.com/yob)) [#1265](https://github.com/buildkite/agent/pull/1265) ([denbeigh2000](https://github.com/denbeigh2000)) [#1255](https://github.com/buildkite/agent/pull/1255) ([yob](https://github.com/yob))
* Fail faster when uploading an artifact > 5Gb to unsupported destinations [#1264](https://github.com/buildkite/agent/pull/1264) ([yob](https://github.com/yob))
* Job should now reliably fail when process.Run() -> err [#1261](https://github.com/buildkite/agent/pull/1261) ([sj26](https://github.com/sj26))
* Fix checkout failure when there is a file called HEAD in the repository root [#1223](https://github.com/buildkite/agent/pull/1223) ([zhenyavinogradov](https://github.com/zhenyavinogradov)) [#1260](https://github.com/buildkite/agent/pull/1260) ([pda](https://github.com/pda))
* Enable `BUILDKITE_AGENT_DEBUG_HTTP` in jobs if it's enabled in the agent process [#1251](https://github.com/buildkite/agent/pull/1251) ([yob](https://github.com/yob))
* Avoid passing nils to Printf() during HTTP Debug mode [#1252](https://github.com/buildkite/agent/pull/1252) ([yob](https://github.com/yob))
* Allow `BUILDKITE_CLEAN_CHECKOUT` to be set via hooks [#1242](https://github.com/buildkite/agent/pull/1242) ([niceking](https://github.com/niceking))
* Add optional brackets to file arg documentation [#1276](https://github.com/buildkite/agent/pull/1276) ([harrietgrace](https://github.com/harrietgrace))
* Reword artifact shasum documentation [#1229](https://github.com/buildkite/agent/pull/1229) ([vineetgopal](https://github.com/vineetgopal))
* Provide example dogstatsd integration options [#1219](https://github.com/buildkite/agent/pull/1219) ([GaryPWhite](https://github.com/GaryPWhite))
* submit basic OS info when registering from a BSD system [#1239](https://github.com/buildkite/agent/pull/1239) ([yob](https://github.com/yob))
* Various typo fixes and light refactors [#1277](https://github.com/buildkite/agent/pull/1277) ([chloeruka](https://github.com/chloeruka)) [#1271](https://github.com/buildkite/agent/pull/1271) ([pda](https://github.com/pda)) [#1244](https://github.com/buildkite/agent/pull/1244) ([pda](https://github.com/pda)) [#1224](https://github.com/buildkite/agent/pull/1224) ([plaindocs](https://github.com/plaindocs))

## [v3.22.1](https://github.com/buildkite/agent/compare/v3.22.0...v3.22.1) (2020-06-18)

### Fixed

- Wrap calls for GCP metadata in a retry [#1230](https://github.com/buildkite/agent/pull/1230) ([jayco](https://github.com/jayco))
- Accept `--experiment` flags in all buildkite-agent subcommands [#1220](https://github.com/buildkite/agent/pull/1220) ([ticky](https://github.com/ticky))
- buildkite/interpolate updated; ability to use numeric default [#1217](https://github.com/buildkite/agent/pull/1217) ([pda](https://github.com/pda))

## [v3.22.0](https://github.com/buildkite/agent/tree/v3.22.0) (2020-05-15)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.21.1...v3.22.0)

### Changed

- Experiment: `normalised-upload-paths` Normalise upload path to Unix/URI form on Windows [#1211](https://github.com/buildkite/agent/pull/1211) (@ticky)
- Improve some outputs for error timers [#1212](https://github.com/buildkite/agent/pull/1212) (@ticky)

## [v3.21.1](https://github.com/buildkite/agent/tree/v3.21.1) (2020-05-05)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.21.0...v3.21.1)

### Fixed

- Rebuild with golang 1.14.2 to fix panic on some linux kernels [#1213](https://github.com/buildkite/agent/pull/1213) (@zifnab06)

## [v3.21.0](https://github.com/buildkite/agent/tree/v3.21.0) (2020-05-05)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.20.0...v3.21.0)

### Fixed

- Add a retry for errors during artifact search [#1210](https://github.com/buildkite/agent/pull/1210) (@lox)
- Fix checkout dir missing and hooks failing after failed checkout retries [#1192](https://github.com/buildkite/agent/pull/1192) (@sj26)

### Changed

- Bump golang build version to 1.14 [#1197](https://github.com/buildkite/agent/pull/1197) (@yob)
- Added 'spawn=1' with to all .cfg templates [#1175](https://github.com/buildkite/agent/pull/1175) (@drnic)
- Send more signal information back to Buildkite [#899](https://github.com/buildkite/agent/pull/899) (@lox)
- Updated artifact --help documentation [#1183](https://github.com/buildkite/agent/pull/1183) (@pda)
- Remove vendor in favor of go modules [#1117](https://github.com/buildkite/agent/pull/1117) (@lox)
- Update crypto [#1194](https://github.com/buildkite/agent/pull/1194) (@gavinelder)

## [v3.20.0](https://github.com/buildkite/agent/tree/v3.20.0) (2020-02-10)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.19.0...v3.20.0)

### Changed

- Multiple plugins can provide checkout & command hooks [#1161](https://github.com/buildkite/agent/pull/1161) (@pda)

## [v3.19.0](https://github.com/buildkite/agent/tree/v3.19.0) (2020-01-30)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.18.0...v3.19.0)

### Fixed

- Fix plugin execution being skipped with duplicate hook warning [#1156](https://github.com/buildkite/agent/pull/1156) (@toolmantim)
- minor changes [#1151](https://github.com/buildkite/agent/pull/1151) [#1154](https://github.com/buildkite/agent/pull/1154) [#1149](https://github.com/buildkite/agent/pull/1149)

## [v3.18.0](https://github.com/buildkite/agent/tree/v3.18.0) (2020-01-21)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.17.0...v3.18.0)

### Added

- Hooks can be written in PowerShell [#1122](https://github.com/buildkite/agent/pull/1122) (@pdemirdjian)

### Changed

- Ignore multiple checkout plugin hooks [#1135](https://github.com/buildkite/agent/pull/1135) (@toolmantim)
- clicommand/annotate: demote success log from Info to Debug [#1141](https://github.com/buildkite/agent/pull/1141) (@pda)

### Fixed

- Fix AgentPool to disconnect if AgentWorker.Start fails [#1146](https://github.com/buildkite/agent/pull/1146) (@keithduncan)
- Fix run-parts usage for CentOS docker entrypoint [#1139](https://github.com/buildkite/agent/pull/1139) (@moensch)

## [v3.17.0](https://github.com/buildkite/agent/tree/v3.17.0) (2019-12-11)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.16.0...v3.17.0)

### Added

- CentOS 7.x Docker image [#1137](https://github.com/buildkite/agent/pull/1137) (@moensch)
- Added --acquire-job for optionally accepting a specific job [#1138](https://github.com/buildkite/agent/pull/1138) (@keithpitt)
- Add filter to remove passwords, etc from job output [#1109](https://github.com/buildkite/agent/pull/1109) (@dbaggerman)
- Allow fetching arbitrary tag=suffix pairs from GCP/EC2 meta-data [#1067](https://github.com/buildkite/agent/pull/1067) (@plasticine)

### Fixed

- Propagate signals in intermediate bash shells [#1116](https://github.com/buildkite/agent/pull/1116) (@lox)
- Detect ansi clear lines and add ansi timestamps in ansi-timestamps experiments [#1128](https://github.com/buildkite/agent/pull/1128) (@lox)
- Added v3 for better go module support [#1115](https://github.com/buildkite/agent/pull/1115) (@sayboras)
- Convert windows paths to unix ones on artifact download [#1113](https://github.com/buildkite/agent/pull/1113) (@lox)

## [v3.16.0](https://github.com/buildkite/agent/tree/v3.16.0) (2019-10-14)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.15.2...v3.16.0)

### Added

- Add ANSI timestamp output experiment [#1103](https://github.com/buildkite/agent/pull/1103) (@lox)

### Changed

- Bump golang build version to 1.13 [#1107](https://github.com/buildkite/agent/pull/1107) (@lox)
- Drop support for setting process title [#1106](https://github.com/buildkite/agent/pull/1106) (@lox)

### Fixed

- Avoid destroying the checkout on specific git errors [#1104](https://github.com/buildkite/agent/pull/1104) (@lox)

## [v3.15.2](https://github.com/buildkite/agent/tree/v3.15.2) (2019-10-10)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.15.1...v3.15.2)

### Added

- Support GS credentials via BUILDKITE_GS_APPLICATION_CREDENTIALS [#1093](https://github.com/buildkite/agent/pull/1093) (@GaryPWhite)
- Add --include-retried-jobs to artifact download/shasum [#1101](https://github.com/buildkite/agent/pull/1101) (@toolmantim)

## [v3.15.1](https://github.com/buildkite/agent/tree/v3.15.1) (2019-09-30)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.15.0...v3.15.1)

### Fixed

- Fix a race condition that causes panics on job accept [#1095](https://github.com/buildkite/agent/pull/1095) (@lox)

## [v3.15.0](https://github.com/buildkite/agent/tree/v3.15.0) (2019-09-17)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.14.0...v3.15.0)

### Changed

- Let the agent serve a status page via HTTP. [#1066](https://github.com/buildkite/agent/pull/1066) (@philwo)
- Only execute the "command" hook once. [#1055](https://github.com/buildkite/agent/pull/1055) (@philwo)
- Fix goroutine leak and memory leak after job finishes [#1084](https://github.com/buildkite/agent/pull/1084) (@lox)
- Allow gs_downloader to use GS_APPLICATION_CREDENTIALS [#1086](https://github.com/buildkite/agent/pull/1086) (@GaryPWhite)
- Updates to `step update` and added `step get` [#1083](https://github.com/buildkite/agent/pull/1083) (@keithpitt)

## [v3.14.0](https://github.com/buildkite/agent/tree/v3.14.0) (2019-08-16)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.13.2...v3.14.0)

### Fixed

- Fix progress group id in debug output [#1074](https://github.com/buildkite/agent/pull/1074) (@lox)
- Avoid os.Exit in pipeline upload command [#1070](https://github.com/buildkite/agent/pull/1070) (@lox)
- Reword the cancel-grace-timeout config option [#1071](https://github.com/buildkite/agent/pull/1071) (@toolmantim)
- Correctly log last successful heartbeat time. [#1065](https://github.com/buildkite/agent/pull/1065) (@philwo)
- Add a test that BUILDKITE_GIT_SUBMODULES is handled [#1054](https://github.com/buildkite/agent/pull/1054) (@lox)

### Changed

- Added feature to enable encryption at rest for artifacts in S3. [#1072](https://github.com/buildkite/agent/pull/1072) (@wolfeidau)
- If commit is HEAD, always use FETCH_HEAD in agent checkout [#1064](https://github.com/buildkite/agent/pull/1064) (@matthewd)
- Updated `--help` output in the README and include more stuff in the "Development" section [#1061](https://github.com/buildkite/agent/pull/1061) (@keithpitt)
- Allow signal agent sends to bootstrap on cancel to be customized [#1041](https://github.com/buildkite/agent/pull/1041) (@lox)
- buildkite/pipeline.yaml etc in pipeline upload default search [#1051](https://github.com/buildkite/agent/pull/1051) (@pda)
- Move plugin tests to table-driven tests [#1048](https://github.com/buildkite/agent/pull/1048) (@lox)

## [v3.13.2](https://github.com/buildkite/agent/tree/v3.13.2) (2019-07-20)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.13.1...v3.13.2)

### Changed

- Fix panic on incorrect token [#1046](https://github.com/buildkite/agent/pull/1046) (@lox)
- Add artifactory vars to artifact upload --help output [#1042](https://github.com/buildkite/agent/pull/1042) (@harrietgrace)
- Fix buildkite-agent upload with absolute path (regression in v3.11.1) [#1044](https://github.com/buildkite/agent/pull/1044) (@petercgrant)
- Don't show vendored plugin header if none are present [#984](https://github.com/buildkite/agent/pull/984) (@lox)

## [v3.13.1](https://github.com/buildkite/agent/tree/v3.13.1) (2019-07-08)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.13.0...v3.13.1)

### Changed

- Add meta-data keys command [#1039](https://github.com/buildkite/agent/pull/1039) (@lox)
- Fix bug where file upload hangs and add a test [#1036](https://github.com/buildkite/agent/pull/1036) (@lox)
- Fix memory leak in artifact uploading with FormUploader [#1033](https://github.com/buildkite/agent/pull/1033) (@lox)
- Add profile option to all cli commands [#1032](https://github.com/buildkite/agent/pull/1032) (@lox)

## [v3.13.0](https://github.com/buildkite/agent/tree/v3.13.0) (2019-06-12)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.12.0...v3.13.0)

### Changed

- Quote command to git submodule foreach to fix error with git 2.20.0 [#1029](https://github.com/buildkite/agent/pull/1029) (@lox)
- Refactor api package to an interface [#1020](https://github.com/buildkite/agent/pull/1020) (@lox)

## [v3.12.0](https://github.com/buildkite/agent/tree/v3.12.0) (2019-05-22)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.11.5...v3.12.0)

### Added

- Add checksums for artifactory uploaded artifacts [#961](https://github.com/buildkite/agent/pull/961) (@lox)
- Add BUILDKITE_GCS_PATH_PREFIX for the path of GCS artifacts [#1000](https://github.com/buildkite/agent/pull/1000) (@DoomGerbil)

### Changed

- Don't force set the executable bit on scripts to be set [#1001](https://github.com/buildkite/agent/pull/1001) (@kuroneko)
- Deprecate disconnect-after-job-timeout [#1009](https://github.com/buildkite/agent/pull/1009) (@lox)
- Update Ubuntu docker image to docker-compose 1.24 [#1005](https://github.com/buildkite/agent/pull/1005) (@pecigonzalo)
- Update Artifactory path parsing to support windows [#1013](https://github.com/buildkite/agent/pull/1013) (@GaryPWhite)
- Refactor: Move signal handling out of AgentPool and into start command [#1012](https://github.com/buildkite/agent/pull/1012) (@lox)
- Refactor: Simplify how we handle idle timeouts [#1010](https://github.com/buildkite/agent/pull/1010) (@lox)
- Remove api socket proxy experiment [#1019](https://github.com/buildkite/agent/pull/1019) (@lox)
- Remove msgpack experiment [#1015](https://github.com/buildkite/agent/pull/1015) (@lox)

## [v3.11.5](https://github.com/buildkite/agent/tree/v3.11.5) (2019-05-13)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.11.4...v3.11.5)

### Fixed

- Fix broken signal handling [#1011](https://github.com/buildkite/agent/pull/1011) (@lox)

### Changed

- Update Ubuntu docker image to docker-compose 1.24 [#1005](https://github.com/buildkite/agent/pull/1005) (@pecigonzalo)

## [v3.11.4](https://github.com/buildkite/agent/tree/v3.11.4) (2019-05-08)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.11.3...v3.11.4)

### Changed

- Fix bug where disconnect-after-idle stopped working [#1004](https://github.com/buildkite/agent/pull/1004) (@lox)

## [v3.11.3](https://github.com/buildkite/agent/tree/v3.11.3) (2019-05-08)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.11.2...v3.11.3)

### Fixed

- Prevent host tags from overwriting aws/gcp tags [#1002](https://github.com/buildkite/agent/pull/1002) (@lox)

### Changed

- Replace signalwatcher package with os/signal [#998](https://github.com/buildkite/agent/pull/998) (@lox)
- Only trigger idle disconnect if all workers are idle [#999](https://github.com/buildkite/agent/pull/999) (@lox)

## [v3.11.2](https://github.com/buildkite/agent/tree/v3.11.2) (2019-04-20)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.11.1...v3.11.2)

### Changed

- Send logging to stderr again [#996](https://github.com/buildkite/agent/pull/996) (@lox)

## [v3.11.1](https://github.com/buildkite/agent/tree/v3.11.1) (2019-04-20)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.11.0...v3.11.1)

### Fixed

- Ensure heartbeats run until agent is stopped [#992](https://github.com/buildkite/agent/pull/992) (@lox)
- Revert "Refactor AgentConfiguration into JobRunnerConfig" to fix error accepting jobs[#993](https://github.com/buildkite/agent/pull/993) (@lox)

## [v3.11.0](https://github.com/buildkite/agent/tree/v3.11.0) (2019-04-16)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.10.4...v3.11.0)

### Fixed

- Allow applying ec2 tags when config tags are empty [#990](https://github.com/buildkite/agent/pull/990) (@vanstee)
- Upload Artifactory artifacts to correct path [#989](https://github.com/buildkite/agent/pull/989) (@GaryPWhite)

### Changed

- Combine apache and nginx sources for mime types. [#988](https://github.com/buildkite/agent/pull/988) (@blueimp)
- Support log output in json [#966](https://github.com/buildkite/agent/pull/966) (@lox)
- Add git-fetch-flags [#957](https://github.com/buildkite/agent/pull/957) (@lox)

## [v3.10.4](https://github.com/buildkite/agent/tree/v3.10.4) (2019-04-05)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.10.3...v3.10.4)

### Fixed

- Fix bug where logger was defaulting to debug [#974](https://github.com/buildkite/agent/pull/974) (@lox)
- Fix race condition between stop/cancel and register [#971](https://github.com/buildkite/agent/pull/971) (@lox)
- Fix incorrect artifactory upload url [#977](https://github.com/buildkite/agent/pull/977) (@GaryPWhite)

## [v3.10.3](https://github.com/buildkite/agent/tree/v3.10.3) (2019-04-02)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.10.2...v3.10.3)

### Fixed

- Fix bug where ec2 tags aren't added correctly [#970](https://github.com/buildkite/agent/pull/970) (@lox)
- Fix bug where host tags overwrite other tags [#969](https://github.com/buildkite/agent/pull/969) (@lox)

## [v3.10.2](https://github.com/buildkite/agent/tree/v3.10.2) (2019-03-31)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.10.1...v3.10.2)

### Fixed

- Update artifatory uploader to use the correct PUT url [#960](https://github.com/buildkite/agent/pull/960) (@GaryPWhite)

### Changed

- Refactor: Move logger.Logger to an interface [#962](https://github.com/buildkite/agent/pull/962) (@lox)
- Refactor: Move AgentWorker construction and registration out of AgentPool [#956](https://github.com/buildkite/agent/pull/956) (@lox)

## [v3.10.1](https://github.com/buildkite/agent/tree/v3.10.1) (2019-03-24)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.10.0...v3.10.1)

### Fixed

- Fix long urls for artifactory integration [#955](https://github.com/buildkite/agent/pull/955) (@GaryPWhite)

## [v3.10.0](https://github.com/buildkite/agent/tree/v3.10.0) (2019-03-12)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.9.1...v3.10.0)

### Added

- Experimental shared repositories (git mirrors) between checkouts [#936](https://github.com/buildkite/agent/pull/936) (@lox)
- Support disconnecting agent after it's been idle for a certain time period [#932](https://github.com/buildkite/agent/pull/932) (@lox)

### Changed

- Restart agents on SIGPIPE from systemd in systemd units [#945](https://github.com/buildkite/agent/pull/945) (@lox)

## [v3.9.1](https://github.com/buildkite/agent/tree/v3.9.1) (2019-03-06)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.9.0...v3.9.1)

### Changed

- Allow the Agent API to reject header times [#938](https://github.com/buildkite/agent/pull/938) (@sj26)
- Increase pipeline upload retries on 5xx errors [#937](https://github.com/buildkite/agent/pull/937) (@toolmantim)
- Pass experiment environment vars to bootstrap [#933](https://github.com/buildkite/agent/pull/933) (@lox)

## [v3.9.0](https://github.com/buildkite/agent/tree/v3.9.0) (2019-02-23)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.8.4...v3.9.0)

### Added

- Artifactory artifact support [#924](https://github.com/buildkite/agent/pull/924) (@GaryPWhite)
- Add a `--tag-from-gcp-labels` for loading agent tags from GCP [#930](https://github.com/buildkite/agent/pull/930) (@conorgil)
- Add a `--content-type` to `artifact upload` to allow specifying a content type [#912](https://github.com/buildkite/agent/pull/912) (@lox)
- Filter env used for command config out of environment [#908](https://github.com/buildkite/agent/pull/908) (@lox)
- If BUILDKITE_REPO is empty, skip checkout [#909](https://github.com/buildkite/agent/pull/909) (@lox)

### Changed

- Terminate bootstrap with unhandled signal after cancel [#890](https://github.com/buildkite/agent/pull/890) (@lox)

### Fixed

- Fix a race condition in cancellation [#928](https://github.com/buildkite/agent/pull/928) (@lox)
- Make sure checkout is removed on failure [#916](https://github.com/buildkite/agent/pull/916) (@lox)
- Ensure TempDir exists to avoid errors on windows [#915](https://github.com/buildkite/agent/pull/915) (@lox)
- Flush output immediately if timestamp-lines not on [#931](https://github.com/buildkite/agent/pull/931) (@lox)

## [v3.8.4](https://github.com/buildkite/agent/tree/v3.8.4) (2019-01-22)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.8.3...v3.8.4)

### Fixed

- Fix and test another seg fault in the artifact searcher [#901](https://github.com/buildkite/agent/pull/901) (@lox)
- Fix a seg-fault in the artifact uploader [#900](https://github.com/buildkite/agent/pull/900) (@lox)

## [v3.8.3](https://github.com/buildkite/agent/tree/v3.8.3) (2019-01-18)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.8.2...v3.8.3)

### Fixed

- Retry forever to upload job chunks [#898](https://github.com/buildkite/agent/pull/898) (@keithpitt)
- Resolve ssh hostname aliases before running ssh-keyscan [#889](https://github.com/buildkite/agent/pull/889) (@ticky)

## [v3.8.2](https://github.com/buildkite/agent/tree/v3.8.2) (2019-01-11)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.8.1...v3.8.2)

### Changed

- Fix another segfault in artifact download [#893](https://github.com/buildkite/agent/pull/893) (@lox)

## [v3.8.1](https://github.com/buildkite/agent/tree/v3.8.1) (2019-01-11)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.8.0...v3.8.1)

### Fixed

- Fixed two segfaults caused by missing loggers [#892](https://github.com/buildkite/agent/pull/892) (@lox)

## [v3.8.0](https://github.com/buildkite/agent/tree/v3.8.0) (2019-01-10)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.7.0...v3.8.0)

### Fixed

- Support absolute paths on Windows for config [#881](https://github.com/buildkite/agent/pull/881) (@petemounce)

### Changed

- Show log output colors on Windows 10+ [#885](https://github.com/buildkite/agent/pull/885) (@lox)
- Better cancel signal handling and error messages in output [#860](https://github.com/buildkite/agent/pull/860) (@lox)
- Use windows console groups for process management [#879](https://github.com/buildkite/agent/pull/879) (@lox)
- Support vendored plugins [#878](https://github.com/buildkite/agent/pull/878) (@lox)
- Show agent name in logger output [#880](https://github.com/buildkite/agent/pull/880) (@lox)
- Change git-clean-flags to cleanup submodules [#875](https://github.com/buildkite/agent/pull/875) (@lox)

## [v3.7.0](https://github.com/buildkite/agent/tree/v3.7.0) (2018-12-18)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.6.1...v3.7.0)

### Changed

- Fixed bug where submodules hosts weren't ssh keyscanned correctly [#876](https://github.com/buildkite/agent/pull/876) (@lox)
- Add a default port to metrics-datadog-host [#874](https://github.com/buildkite/agent/pull/874) (@lox)
- Hooks can now modify \$BUILDKITE_REPO before checkout to change the git clone or fetch address [#877](https://github.com/buildkite/agent/pull/877) (@sj26)
- Add a configurable cancel-grace-period [#700](https://github.com/buildkite/agent/pull/700) (@lox)
- Resolve BUILDKITE_COMMIT before pipeline upload [#871](https://github.com/buildkite/agent/pull/871) (@lox)

## [v3.6.1](https://github.com/buildkite/agent/tree/v3.6.1) (2018-12-13)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.6.0...v3.6.1)

### Added

- Add another search path for config file on Windows [#867](https://github.com/buildkite/agent/pull/867) (@petemounce)

### Fixed

- Exclude headers from timestamp-lines output [#870](https://github.com/buildkite/agent/pull/870) (@lox)

## [v3.6.0](https://github.com/buildkite/agent/tree/v3.6.0) (2018-12-04)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.5.4...v3.6.0)

### Fixed

- Fix bug that caused an extra log chunk to be sent in some cases [#845](https://github.com/buildkite/agent/pull/845) (@idledaemon)
- Don't retry checkout on build cancel [#863](https://github.com/buildkite/agent/pull/863) (@lox)
- Add buildkite-agent.cfg to docker images [#847](https://github.com/buildkite/agent/pull/847) (@lox)

### Added

- Experimental `--spawn` option to spawn multiple parallel agents [#590](https://github.com/buildkite/agent/pull/590) (@lox) - **Update:** This feature is now super stable.
- Add a linux/ppc64le build target [#859](https://github.com/buildkite/agent/pull/859) (@lox)
- Basic metrics collection for Datadog [#832](https://github.com/buildkite/agent/pull/832) (@lox)
- Added a `job update` command to make changes to a job [#833](https://github.com/buildkite/agent/pull/833) (@keithpitt)
- Remove the checkout dir if the checkout phase fails [#812](https://github.com/buildkite/agent/pull/812) (@lox)

### Changed

- Add tests around gracefully killing processes [#862](https://github.com/buildkite/agent/pull/862) (@lox)
- Removes process callbacks and moves them to job runner [#856](https://github.com/buildkite/agent/pull/856) (@lox)
- Use a channel to monitor whether process is killed [#855](https://github.com/buildkite/agent/pull/855) (@lox)
- Move to golang 1.11 [#839](https://github.com/buildkite/agent/pull/839) (@lox)
- Add a flag to disable http2 in the start command [#851](https://github.com/buildkite/agent/pull/851) (@lox)
- Use transparent for golang http2 transport [#849](https://github.com/buildkite/agent/pull/849) (@lox)

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

- Experiment: Pass jobs an authenticated unix socket rather than an access token [#759](https://github.com/buildkite/agent/pull/759) (@lox)
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

- Don't set Content-Encoding on s3 upload [#732](@lox)

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

- Refactored bootstrap to be more testable / maintainable [\#514](https://github.com/buildkite/agent/pull/514) [\#530](https://github.com/buildkite/agent/pull/530) [\#536](https://github.com/buildkite/agent/pull/536) [\#522](https://github.com/buildkite/agent/pull/522) (@lox)
- Add BUILDKITE_GCS_ACCESS_HOST for GCS Host choice [\#532](https://github.com/buildkite/agent/pull/532) (@jules2689)
- Prefer plugin, local, global and then default for hooks [\#549](https://github.com/buildkite/agent/pull/549) (@lox)
- Integration tests for v3 [\#548](https://github.com/buildkite/agent/pull/548) (@lox)
- Add docker integration tests [\#547](https://github.com/buildkite/agent/pull/547) (@lox)
- Use latest golang 1.9 [\#541](https://github.com/buildkite/agent/pull/541) (@lox)
- Faster globbing with go-zglob [\#539](https://github.com/buildkite/agent/pull/539) (@lox)
- Consolidate Environment into env package (@lox)

### Fixed

- Fix bug where ssh-keygen error causes agent to block [\#521](https://github.com/buildkite/agent/pull/521) (@lox)
- Pre-exit hook always fires now

## [v3.0-beta.31](https://github.com/buildkite/agent/tree/v3.0-beta.31) (2017-08-14)

[Full Changelog](https://github.com/buildkite/agent/compare/v3.0-beta.30...v3.0-beta.31)

### Fixed

- Support paths in BUILDKITE_ARTIFACT_UPLOAD_DESTINATION [\#519](https://github.com/buildkite/agent/pull/519) (@lox)

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
- Add submodule remotes to known_hosts [\#509](https://github.com/buildkite/agent/pull/509) (@lox)

## 3.0-beta.29 - 2017-07-18

### Added

- Added a `--timestamp-lines` option to `buildkite-agent start` that will insert RFC3339 UTC timestamps at the beginning of each log line. The timestamps are not applied to header lines. [#501](@lox)
- Ctrl-c twice will force kill the agent [#499](@lox)
- Set the content encoding on artifacts uploaded to s3 [#494] (thanks @airhorns)
- Output fetched commit sha during git fetch for pull request [#505](@sj26)

### Changed

- Migrate the aging goamz library to the latest aws-sdk [#474](@lox)

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

- `buildkite-agent start --disconnect-after-job` will run the agent, and automatically disconnect after running its first job. This has sometimes been referred to as "one shot" mode and is useful when you spin up an environment per-job and want the agent to automatically disconnect once it's finished its job
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

- The agent now receives its "job status interval" from the Agent API (the number of seconds between checking if its current job has been remotely canceled)

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
- Substring expansion works for variables in pipeline uploads, like \${BUILDKITE_COMMIT:0:7} will return the first seven characters of the commit SHA (#387)

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
- Added Buildkite's bin dir to the end, not the start, of \$PATH (#267)
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
- Added Buildkite's bin dir to the end, not the start, of \$PATH (#268)
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
- The agent will now change its process title on linux/amd64 machines to report its current status: `buildkite-agent v2.1 (my-agent-name) [job a4f-a4fa4-af4a34f-af4]`

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
