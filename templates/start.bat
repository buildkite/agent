@ECHO off

TITLE Buildkite Agent

REM If the token file already exists, we can skip the setup
IF EXIST token.txt (
  GOTO :Start
)

:Setup
REM Ask the user to enter their agent token
ECHO Please enter your Buildbox agent registration token:
SET AGENT_TOKEN=
SET /p AGENT_TOKEN=

REM Validate that they've entered the token
IF "%AGENT_TOKEN%" == "" (
REM Lol, GOTO
GOTO :Prompt
)

REM Save the token to disk
ECHO.%AGENT_TOKEN%> token.txt

ECHO.
ECHO We've saved your agent token to the `token.txt` file in this folder, so next
ECHO time you start the agent, you won't need to enter it again.
ECHO.
ECHO If you'd like to change the token, you can just edit the `token.txt` file.
ECHO.
PAUSE
ECHO.

:Start

REM Read in the agent token
SET /p AGENT_TOKEN=<token.txt

REM Start the buildkite-agent
CALL buildkite-agent start --token "%AGENT_TOKEN%" --bootstrap-script bootstrap.bat --debug

PAUSE
