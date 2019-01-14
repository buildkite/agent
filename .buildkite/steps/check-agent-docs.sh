#!/bin/bash
set -euo pipefail

get_agent_env_vars() {
  grep -ohr -e 'BUILDKITE_[a-zA-Z0-9_\-]*[a-zA-Z0-9]*' \
    --include '*.go' --exclude "clicommand/bootstrap.go" \
    --exclude '*_test.go' . \
    | grep -v BUILDKITE_X_ | sort | uniq
}

get_docs_env_vars() {
  grep -ohr -e 'BUILDKITE_[a-zA-Z0-9_\-]*[a-zA-Z0-9]' . \
    | sort | uniq
}

get_agent_env_vars > agent_env_vars.txt

(
  [ -d docs ] || git clone https://github.com/buildkite/docs.git
  cd docs
  get_docs_env_vars > ../docs_env_vars.txt
)

undocumented=()
echo "--- 📖 🔍 Checking env in agent are documented"

while read -r env ; do
  echo -n "Checking $env: "

  if grep -q "$env" docs_env_vars.txt ; then
    echo "✅"
  else
    echo "🚨"
    undocumented+=("$env")
  fi
done < agent_env_vars.txt

if [ ${#undocumented[@]} -eq 0 ] ; then
  echo "+++ All documentation up to date! 💃"
else
  for env in "${undocumented[@]}" ; do
    echo "+++ 🚨 $env isn't documented"
    git grep "$env"
    echo
  done
fi
