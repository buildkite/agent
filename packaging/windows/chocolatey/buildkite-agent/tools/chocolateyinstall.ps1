Install-ChocolateyZipPackage -PackageName 'buildkite-agent' `
 -Url 'https://github.com/buildkite/agent/releases/download/v3.0-beta.38/buildkite-agent-windows-386-3.0-beta.38.zip' `
 -UnzipLocation "$(Split-Path -Parent $MyInvocation.MyCommand.Definition)" `
 -Url64 'https://github.com/buildkite/agent/releases/download/v3.0-beta.38/buildkite-agent-windows-amd64-3.0-beta.38.zip'