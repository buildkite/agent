# Artifact object attributes

The agent can attach **S3 object tags**, **GCS object contexts**, or **Azure blob
index tags** when uploading artifacts to customer-owned storage. This is opt-in
and disabled by default. It does not add separate custom metadata, bucket labels,
resource tags, or attributes on Buildkite-managed storage or Artifactory.

These identifiers can support traceability and customer cost-allocation models.
They do not automatically become cloud billing dimensions. Allocating downloads
to consuming pipelines also requires activity data, not just attributes on the
object created by the producing pipeline.

## Settings

Each setting is available in an agent config file and as a flag on both
`buildkite-agent start` and `buildkite-agent artifact upload`:

| Config key / flag | Environment variable | Default |
| --- | --- | --- |
| `object-attributes` | `BUILDKITE_ARTIFACT_UPLOAD_OBJECT_ATTRIBUTES` | `false` |
| `s3-object-tags` | `BUILDKITE_S3_OBJECT_TAGS` | `{}` |
| `gcs-object-contexts` | `BUILDKITE_GCS_OBJECT_CONTEXTS` | `{}` |
| `azure-blob-tags` | `BUILDKITE_AZURE_BLOB_TAGS` | `{}` |

Maps must be JSON objects with string values. They apply to every matched file;
use separate invocations for different attributes. Nulls, non-string values and
trailing JSON are rejected. Repeated JSON keys use the last value. Attributes
and their keys are case-sensitive.

Each setting resolves independently, in this order:

1. An explicit upload CLI flag.
2. The current job/process environment, including hook changes.
3. Agent config defaults, supplied to the job for otherwise absent variables.
4. The built-in default.

Higher-precedence maps **replace** whole maps; they do not merge keys. Use `{}`
to clear an inherited map. An explicit empty string is invalid for an enabled
active provider. `--object-attributes=false` suppresses all maps, even malformed
inherited values, without new request attributes or attribute permissions.
Agent defaults are overridable, not an enforcement policy or trusted provenance.
Protected-environment settings still apply to job-supplied variables.

Only the selected provider's map is validated. Defaults for other providers can
coexist on a mixed-storage agent. With attributes enabled, passing an explicit
flag for a different provider is an error, even if its value is `{}`. Hosted
storage and Artifactory allow enable-only/all-empty maps as a no-op, but reject
nonempty/invalid maps and explicit provider flags. No matching files still
succeeds, without parsing attributes; otherwise local errors fail before cloud
client construction and Buildkite artifact registration.

## Agent defaults

Agent config values are literal JSON: no shell execution, variable expansion or
automatically inserted pipeline/build/job fields.

```ini
object-attributes=true
s3-object-tags='{"team":"payments","cost_centre":"platform"}'
gcs-object-contexts='{"team":"payments","cost_centre":"platform"}'
azure-blob-tags='{"team":"payments","cost_centre":"platform"}'
```

Single-quoted, double-quoted or unquoted JSON is supported for these settings.
JSON escapes and `#` inside strings are preserved. Quote wrappers are config-file
syntax, not part of the JSON. Do not double-escape JSON for the config reader.

Automatic `artifact_paths` uploads and upload commands run within jobs receive
these defaults. A standalone upload does not ask a running agent for its config
or automatically discover its config file. Select one using `artifact upload
--config /path/to/buildkite-agent.cfg` (or `BUILDKITE_AGENT_CONFIG`); normal upload
authentication/job settings are still required.

## Step settings

With the private upload destination and credentials configured separately:

```yaml
steps:
  - label: Build
    command: ./build.sh
    artifact_paths: "dist/**/*"
    env:
      BUILDKITE_ARTIFACT_UPLOAD_OBJECT_ATTRIBUTES: "true"
      BUILDKITE_S3_OBJECT_TAGS: '{"team":"payments","component":"api"}'
```

For GCS or Azure, use the corresponding provider variable. Container plugins
must forward these variables into the upload process. The existing agent-variable
forwarding list includes them, but plugins with explicit environment allowlists
must be configured accordingly. The job JSON environment file still contains
only upstream job settings, not agent defaults.

## Runtime IDs and direct commands

JSON-encode values instead of constructing JSON with string concatenation. For
example, in a Bash command or an environment/pre-artifact hook:

```bash
attributes=$(jq -cn \
  --arg pipeline "$BUILDKITE_PIPELINE_SLUG" \
  --arg build "$BUILDKITE_BUILD_ID" \
  --arg job "$BUILDKITE_JOB_ID" \
  '{pipeline:$pipeline,build:$build,job:$job}')

buildkite-agent artifact upload --object-attributes \
  --s3-object-tags "$attributes" "dist/**/*" \
  "s3://my-artifacts/$BUILDKITE_JOB_ID"
```

`jq` is an example encoder, not an agent dependency. Use `--gcs-object-contexts`
with a `gs://` destination, or `--azure-blob-tags` with an existing supported
`https://<account>.blob.core.windows.net/<container>/` destination.

For automatic uploads, export the provider variable from an environment or
pre-artifact hook. An export inside the command subprocess does not change the
later automatic artifact phase's environment. In pipeline YAML uploaded with
interpolation enabled, escape `$` as `$$` where expansion must happen at job
runtime rather than during pipeline upload. Values must meet the destination's
rules; for example, GCS contexts cannot contain slashes, so choose IDs/slugs
rather than URLs or branch names containing `/`.

## Provider constraints and permissions

The agent checks the following limits locally. Cloud authorization and account
capabilities remain service-enforced. If the provider rejects the upload, the
agent reports a failure; it does not retry without attributes or silently discard
them. Attributes are attached at creation, not by a later tagging request.

- **S3:** at most 10 object tags. Keys are 1–128 UTF-16 units and values 0–256.
  Allowed characters are Unicode letters, numbers, separators and `_ . : / = + -`.
  The `aws:` key prefix is reserved. Requires `s3:PutObjectTagging` in addition to
  existing upload permissions, including for multipart initiation. S3-compatible
  services must support object tagging on their upload APIs.
- **GCS:** at most 50 object contexts, 256 **UTF-8 bytes** per key and value and
  25 KiB combined. Both keys and values are nonempty, start with an alphanumeric
  character and cannot contain single/double quotes or forward/backslashes.
  The `google:` key prefix is reserved. The byte limit follows current
  [quotas](https://docs.cloud.google.com/storage/quotas) and the
  [overview](https://docs.cloud.google.com/storage/docs/object-contexts), despite
  the how-to describing it as characters. Requires
  `storage.objects.createContext` in addition to ordinary object-create
  permissions, not context-update permissions. GCS contexts incur recurring
  charges.
- **Azure:** at most 10 blob index tags. Keys are 1–128 characters and values
  0–256. Allowed characters are ASCII letters, digits, space and `+ - . / : = _`.
  Requires tag-write authorization, such as the RBAC data action
  `Microsoft.Storage/storageAccounts/blobServices/containers/blobs/tags/write`
  (or SAS `t` where supported). Account features such as hierarchical namespace
  (HNS) restrict index-tag support. The existing agent authentication supports
  connection strings, account keys and default credentials; placing a SAS in the
  destination query is not supported.

Object tags/contexts may incur provider charges. Consult current
[S3 pricing](https://aws.amazon.com/s3/pricing/),
[GCS pricing](https://cloud.google.com/storage/pricing) and
[Azure pricing](https://azure.microsoft.com/pricing/details/storage/blobs/), plus
[S3 tagging](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-tagging.html),
[GCS context permissions](https://docs.cloud.google.com/storage/docs/use-object-contexts)
and [Azure index-tag restrictions](https://learn.microsoft.com/en-us/azure/storage/blobs/storage-manage-find-blobs).

Attributes are not a secret store. Validation errors do not print attribute
values, but existing SDK HTTP debug logging can expose request attributes.
