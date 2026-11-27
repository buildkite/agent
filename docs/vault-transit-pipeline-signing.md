# Sign Buildkite pipelines with Vault Transit

Vault Transit signs pipeline steps with a versioned ECDSA P-256 key. The private
key stays in Vault. Buildkite agents verify the resulting ES256 signatures using
a local public JSON Web Key Set (JWKS), without a Vault connection or credentials.

## Set up the signing key and uploader

Create a dedicated `ecdsa-p256` key in an existing Transit mount. This example
uses a nested mount at `ci/transit` and creates version 1:

```sh
vault write ci/transit/keys/pipeline type=ecdsa-p256 exportable=false
```

Give the uploader only the following Vault policy capabilities. Use a separate
operator identity for key creation and rotation.

```hcl
path "ci/transit/keys/pipeline" {
  capabilities = ["read"]
}

path "ci/transit/sign/pipeline" {
  capabilities = ["update"]
}
```

Set the Vault connection settings (such as `VAULT_ADDR`, `VAULT_NAMESPACE`, and
CA settings) in the trusted uploader environment. Use a short-lived token scoped
to the policy above or an access-controlled Vault Proxy. Keep TLS verification
enabled. When `agent start` configures Vault signing, its jobs cannot override
the signing reference, `VAULT_*`, or the standard HTTP proxy and CA variables
through job environment values or pipeline secret mappings. This does not
restrict hooks, plugins, or job code. For standalone signing commands, use a
trusted launcher or environment. Run only trusted code in the uploader and don't
set a shared `VAULT_TOKEN` in the agent service environment, which local child
processes can inherit. Uploaders in containers need their own trusted Vault
connection and authentication setup.

## Publish the public verification key

Use the same pinned reference to generate the verification key and configure
the uploader. This shell function uses the Vault CLI and Node.js to read that
version's public key and print a one-key JWKS. When rotating, merge the new key
into the existing set instead of overwriting it.

```sh
vault_transit_jwks() {
  node - "$1" <<'JS'
const { execFileSync } = require('node:child_process');
const { createHash, createPublicKey } = require('node:crypto');
const ref = process.argv[2];
const match = /^(.+\/keys\/[^/@]+)@([1-9]\d*)$/.exec(ref);
if (!match) throw new Error('Expected <mount>/keys/<name>@<positive-version>');
const [, path, version] = match;
const { data } = JSON.parse(execFileSync('vault', ['read', '-format=json', path], { encoding: 'utf8' }));
const pem = data.keys?.[version]?.public_key;
if (!pem) throw new Error(`No public key for ${ref}`);
const { crv, kty, x, y } = createPublicKey(pem).export({ format: 'jwk' });
if (kty !== 'EC' || crv !== 'P-256') throw new Error('Expected an ECDSA P-256 public key');
const publicJwk = { crv, kty, x, y };
const kid = createHash('sha256').update(JSON.stringify(publicJwk)).digest('base64url');
console.log(JSON.stringify({ keys: [{ ...publicJwk, alg: 'ES256', kid }] }, null, 2));
JS
}

signing_ref=ci/transit/keys/pipeline@1
vault_transit_jwks "$signing_ref" > pipeline-v1.jwks
```

Each public entry has `kty: "EC"`, `crv: "P-256"`, `x`, `y`, `alg: "ES256"`,
and `kid`. The `kid` is the [RFC 7638 thumbprint](https://www.rfc-editor.org/rfc/rfc7638):
SHA-256 of the compact UTF-8 JSON containing just `crv`, `kty`, `x`, and `y`,
in that order, encoded as unpadded base64url. It must match the `kid` in the
protected signature header; it is not the Vault key name or version.

Place the JWKS at a trusted path on each verifying agent and make sure jobs
cannot modify it. This `buildkite-agent.cfg` example configures both signing and
verification. A verify-only agent omits `signing-vault-transit-key`.

```ini
signing-vault-transit-key="ci/transit/keys/pipeline@1"
verification-jwks-file="/etc/buildkite-agent/pipeline-verification.jwks"
verification-failure-behavior="block"
```

`buildkite-agent start` passes the signing reference to job uploaders and only
checks its format at startup. `pipeline upload` and `tool sign` also accept
`--signing-vault-transit-key` or `BUILDKITE_AGENT_SIGNING_VAULT_TRANSIT_KEY`. The
reference must have the form `<mount>/keys/<name>@<positive-version>`; `latest`
is not supported. On the same command, don't combine it with a JWKS, AWS KMS,
or GCP KMS **signing** option. A **verification** JWKS is separate.

To sign a local pipeline before installing it in Buildkite, give `tool sign`
the exact repository URL configured for that pipeline:

```sh
buildkite-agent tool sign --signing-vault-transit-key "$signing_ref" \
  --repo https://git.example.com/your-team/your-repo.git pipeline.yml > signed-pipeline.yml
```

## Publish both verification keys before switching signers

1. Rotate the Transit key with `vault write -f ci/transit/keys/pipeline/rotate`.
   Confirm the new version in the key metadata. Uploaders pinned to the old
   version keep using it as long as Vault still permits signing with it.
2. Set `next_signing_ref` to the exact reference you will deploy to uploaders.
   Use it to generate the new public JWK and merge it into the current
   verification set with `jq`, retaining all keys still needed:

   ```sh
   next_signing_ref=ci/transit/keys/pipeline@2
   vault_transit_jwks "$next_signing_ref" > pipeline-v2.jwks &&
     jq -s '{keys: (map(.keys) | add | unique_by(.kid))}' \
       pipeline-verification.jwks pipeline-v2.jwks > pipeline-verification-next.jwks
   ```

   Deploy the merged set to **all** verifying agents and restart them so they
   load it. Obtain the expected new `kid` with `jq -er '.keys[0].kid' pipeline-v2.jwks`.
   Keep signers pinned to the old version until every verifier has restarted
   with a JWKS containing both the old `kid` and the expected new one.
3. Switch uploaders to the value of `next_signing_ref` (restart those configured
   through `agent start`). Re-sign any stored pipeline definitions that still
   need to run. Confirm new and old signatures verify during the overlap. To
   roll back signing, pin uploaders to the old version while both public keys
   are still deployed and Vault still permits signing with the old version.
4. Remove the old public JWK and restart verifiers only after nothing needs to
   verify an old signature. Account for queued or retried jobs, stored pipeline
   definitions, and any remaining old uploaders. Old signatures no longer verify
   once that public key is removed.
