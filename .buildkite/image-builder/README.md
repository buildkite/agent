image-builder
=============

Assets related to https://github.com/buildkite/image-builder

## `bootstrap.ps`

This is a PowerShell bootstrap script for Windows elastic-stack agents used
to test buildkite-agent.

At time of writing it depends on a not-yet-merged [windows branch](https://github.com/buildkite/image-builder/pull/4)
of image-builder.

It is manually copied to an S3 bucket for use by the associated image-builder stack:

```sh
aws s3 cp bootstrap.ps1 s3://buildkite-agent-image-builder/windows/bootstrap.ps1
```
