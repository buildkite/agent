$ErrorActionPreference = "Stop"

go version
go install gotest.tools/gotestsum@v1.8.0

echo '+++ Installing Ruby for integration tests'
choco install -y ruby --version "3.1.3.1"
If ($lastexitcode -ne 0) { Exit $lastexitcode }

refreshenv
If ($lastexitcode -ne 0) { Exit $lastexitcode }

echo '+++ Running tests'
gotestsum --junitfile "junit-${BUILDKITE_JOB_ID}.xml" -- -count=1 -failfast ./...
If ($lastexitcode -ne 0) { Exit $lastexitcode }

echo '+++ Running integration tests for git-mirrors experiment'
$Env:TEST_EXPERIMENT = "git-mirrors"; gotestsum --junitfile "junit-${BUILDKITE_JOB_ID}-git-mirrors.xml" -- -count=1 -failfast ./bootstrap/integration
If ($lastexitcode -ne 0) { Exit $lastexitcode }
