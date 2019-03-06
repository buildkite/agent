## Development docker image for buildkite-agent on windows
FROM mcr.microsoft.com/windows/servercore:1809

ENV ChocolateyUseWindowsCompression false

# Set your PowerShell execution policy
RUN powershell Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Force

# Install Chocolatey
RUN powershell -NoProfile -ExecutionPolicy Bypass -Command "iex ((New-Object System.Net.WebClient).DownloadString('https://chocolatey.org/install.ps1'))" && SET "PATH=%PATH%;%ALLUSERSPROFILE%\chocolatey\bin"

# Install Chocolatey packages
RUN choco install -ry git.install -params '"/GitAndUnixToolsOnPath"' && \
  choco install -ry openssh && \
  choco install golang --version 1.12 -mry && \
  choco install -ry mingw

WORKDIR c:/Users/ContainerAdministrator/go/src/github.com/buildkite/agent

# Copy the rest of the code
COPY . ./
