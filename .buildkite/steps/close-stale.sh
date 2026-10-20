#!/usr/bin/env bash
set -euo pipefail
# Neither OIDC nor GitHub tokens should appear in command traces.
set +x

echo '--- :github: Requesting a short-lived GitHub token from Exchange'
oidc_token=$(buildkite-agent oidc request-token --audience exchange --claim organization_id --claim pipeline_id)
response_file=$(mktemp)
trap 'rm -f -- "$response_file"' EXIT

# Invoke on the host, which has the agent and AWS CLI, before starting Docker.
# Pass the OIDC payload through stdin rather than exposing it in process arguments.
metadata=$(jq -Rcn '{token: input, github_organization: "buildkite"}' <<< "$oidc_token" |
  aws lambda invoke \
    --region us-east-1 \
    --function-name arn:aws:lambda:us-east-1:032379705303:function:exchange \
    --cli-binary-format raw-in-base64-out \
    --payload fileb:///dev/stdin \
    --log-type None \
    --output json \
    "$response_file")

if ! jq -e '.StatusCode == 200 and .FunctionError == null' <<< "$metadata" >/dev/null; then
  echo 'Exchange Lambda invocation failed; check the Lambda logs.' >&2
  exit 1
fi

# Fail closed, without logging a response that could contain credentials.
if ! GH_TOKEN=$(jq -er 'select(.error == null) | .token | select(type == "string" and test("\\A[^[:cntrl:][:space:]]+\\z"))' "$response_file" 2>/dev/null); then
  echo 'Exchange returned no usable GitHub token; check the Exchange policy and Lambda logs.' >&2
  exit 1
fi

export GH_TOKEN
echo '--- :broom: Running stale cleanup'
docker run --rm \
  --env GH_TOKEN \
  --volume "$PWD:/work" \
  --workdir /work \
  golang:1.26.5 \
  go run ./scripts/close-stale -months="${STALE_MONTHS}" -dry-run="${STALE_DRY_RUN}"
