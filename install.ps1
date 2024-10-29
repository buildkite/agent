$installDir = "C:\buildkite-agent"
$beta = $env:buildkiteAgentBeta
$token = $env:buildkiteAgentToken
$tags = $env:buildkiteAgentTags
$url = $env:buildkiteAgentUrl
$version = $env:buildkiteAgentVersion


if ($(Get-ComputerInfo -Property OsArchitecture).OsArchitecture -eq "ARM 64-bit Processor") {
  $arch = "arm64"
} else {
  # The value is "64-bit" on my intel laptop with windows in a virtualbox VM
  $arch = "amd64"
}

if ([string]::IsNullOrEmpty($token)) {
    throw "No token specified, set `$env:buildkiteAgentToken"
}

$ErrorActionPreference = "Stop"

Write-Host "
  _           _ _     _ _    _ _                                _
 | |         (_) |   | | |  (_) |                              | |
 | |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_
 | '_ \| | | | | |/ _\` | |/ / | __/ _ \  / _\` |/ _\` |/ _ \ '_ \| __|
 | |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_
 |_.__/ \__,_|_|_|\__,_|_|\_\_|\__\___|  \__,_|\__, |\___|_| |_|\__|
                                                __/ |
                                               |___/"

## Verify we are elevated
## https://superuser.com/questions/749243/detect-if-powershell-is-running-as-administrator

$elevated = [bool](([System.Security.Principal.WindowsIdentity]::GetCurrent()).groups -match "S-1-5-32-544")
if($elevated -eq $false) {
    throw "In order to install services, please run this script elevated."
}

if ([string]::IsNullOrEmpty($url)) {
    if($null -eq $version){
      $version = "latest"
    }
    if ($version -eq "latest") {
      $releaseInfoUrl = "https://buildkite.com/agent/releases/$($version)?platform=windows&arch=$arch"
      if($beta) {
          $releaseInfoUrl = $releaseInfoUrl + "&prerelease=true"
      }
      Write-Host "Finding latest release"
  
      $resp = Invoke-WebRequest -Uri "$releaseInfoUrl" -UseBasicParsing -Method GET
  
      $releaseInfo = @{}
      foreach ($line in $resp.Content.Split("`n")) {
          $info = $line -split "="
          $releaseInfo.add($info[0],$info[1])
      }
      $url = $releaseInfo.url
    } else {
      $url = "https://github.com/buildkite/agent/releases/download/v$($version)/buildkite-agent-windows-$arch-$($version).zip"
    }
}

# Github requires TLS1.2
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

Write-Host "Downloading $url"
Invoke-WebRequest -Uri $url -OutFile 'buildkite-agent.zip'

if (Test-Path -Path $installDir) {
    $permissions = (Get-Acl $installDir).Access |
        where {$_.IdentityReference -match 'BUILTIN\\Users' -and `
            $_.AccessControlType -eq [System.Security.AccessControl.AccessControlType]::Allow}
    if ($permissions) {
        Write-Host "
WARNING: All users have the following access to the installation directory '$installDir':
WARNING:   $($permissions.FileSystemRights)
WARNING: Consider only allowing administrators access to this directory.
        "
    }
} else {
    Write-Host "Restricting installation directory access to Administrators: '$installDir'"
    New-Item -ItemType "directory" -Path $installDir | Out-Null
    $acl = Get-Acl $installDir
    # Disable ACL inheritance and remove existing inherited rules
    $acl.SetAccessRuleProtection($true,$false)
    # Allow System and Administrators full access
    $rule = New-Object System.Security.AccessControl.FileSystemAccessRule("NT AUTHORITY\SYSTEM","FullControl","Allow")
    $acl.AddAccessRule($rule)
    $rule = New-Object System.Security.AccessControl.FileSystemAccessRule("BUILTIN\Administrators","FullControl","Allow")
    $acl.AddAccessRule($rule)
    $acl | Set-Acl $installDir
}

Write-Host 'Expanding buildkite-agent.zip'
Expand-Archive -Force -Path buildkite-agent.zip -DestinationPath $installDir
Remove-Item buildkite-agent.zip -Force

$binDir = Join-Path $installDir "bin"
if (![System.IO.Directory]::Exists($binDir)) {[void][System.IO.Directory]::CreateDirectory($binDir)}

Write-Host 'Expanding buildkite-agent.exe into bin'
Join-Path $installDir "buildkite-agent.exe" | Move-item -Destination $binDir -Force

Write-Host 'Updating PATH'
$env:PATH = "${binDir};" + $env:PATH
[Environment]::SetEnvironmentVariable('PATH', $env:PATH, [EnvironmentVariableTarget]::Machine)

# Verify it worked
buildkite-agent --version

Write-Host "Updating configuration in ${installDir}\buildkite-agent.cfg"
$buildkiteAgentCfgTemplate = Get-Content "${installDir}\buildkite-agent.cfg"
$buildkiteAgentCfgTemplate = $buildkiteAgentCfgTemplate -replace 'token="xxx"', ('token="{0}"' -f $token.Trim())

if (![string]::IsNullOrEmpty($tags)) {
    $buildkiteAgentCfgTemplate = $buildkiteAgentCfgTemplate -replace '# tags="key1=val2,key2=val2"', ('tags="{0}"' -f $tags)
}

[System.IO.File]::WriteAllLines("${installDir}\buildkite-agent.cfg", $buildkiteAgentCfgTemplate);

Write-Host "Successfully installed to $installDir

You can now start the agent!

  ${binDir}\buildkite-agent.exe start

For docs, help and support:

  https://buildkite.com/docs/agent/v3

Happy building! <3
"
