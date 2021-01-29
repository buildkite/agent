Write-Output "Installing Go"
choco install golang --yes --version 1.15.1
$Env:Path = "C:\Go\bin;" + $Env:Path

# Set up a system-wide GOPATH so that e.g. Administrator can pre-fetch Go modules
# which are later used by buildkite-agent.
Write-Output "Configuring shared GOPATH for all users"
$Env:GOPATH = "C:\GoPath"
$Env:Path = "C:\GoPath\bin;" + $Env:Path
[Environment]::SetEnvironmentVariable('GOPATH', $Env:GOPATH, [EnvironmentVariableTarget]::Machine)
[Environment]::SetEnvironmentVariable('Path', $env:PATH, [EnvironmentVariableTarget]::Machine)

Write-Output "Configuring core.symlinks = true"
git config --system core.symlinks true

Write-Output "github.com/buildkite/agent: cloning into git-mirrors"
git clone -v --mirror -- `
  "git://github.com/buildkite/agent.git" `
  "C:\buildkite-agent\git-mirrors\git-github-com-buildkite-agent-git"

Write-Output "github.com/buildkite/agent: cloning temporary work tree"
git clone -v `
  --reference "C:\buildkite-agent\git-mirrors\git-github-com-buildkite-agent-git" `
  "git://github.com/buildkite/agent.git" `
  "C:\buildkite-image-builder\agent"

Write-Output "github.com/buildkite/agent: go mod download"
Set-Location \buildkite-image-builder\agent
go mod download -x

Write-Output "pre-installing gotest.tools/gotestsum"
go get -v gotest.tools/gotestsum
