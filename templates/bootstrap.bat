@echo off

REM enable delayed expansion so that ERRORLEVEL is evaluated properly inside IF blocks
SETLOCAL ENABLEDELAYEDEXPANSION

echo ~~~ Preparing build folder

SET SANITIZED_PROJECT_SLUG=%BUILDKITE_PROJECT_SLUG:/=\%
SET BUILDKITE_BUILD_DIR=%BUILDKITE_BUILD_PATH%\%BUILDKITE_AGENT_NAME%\%SANITIZED_PROJECT_SLUG%

REM Add the BUILDKITE_BIN_PATH to the PATH

SET PATH=%PATH%;%BUILDKITE_BIN_PATH%

REM Create the build directory

IF NOT EXIST "%BUILDKITE_BUILD_DIR%" (
  REM Create the build directory

  ECHO ^> MKDIR "%BUILDKITE_BUILD_DIR%"
  MKDIR "%BUILDKITE_BUILD_DIR%"
  IF !ERRORLEVEL! NEQ 0 EXIT !ERRORLEVEL!
)

REM Move to the build directory

ECHO ^> CD /D "%BUILDKITE_BUILD_DIR%"
CD /D "%BUILDKITE_BUILD_DIR%"
IF %ERRORLEVEL% NEQ 0 EXIT %ERRORLEVEL%

REM Do we need to do a git checkout?

IF NOT EXIST ".git" (
  ECHO ^> git clone %BUILDKITE_REPO%
  CALL git clone "%BUILDKITE_REPO%" . -v
  IF !ERRORLEVEL! NEQ 0 EXIT !ERRORLEVEL!
)

REM Clean the repo

ECHO ^> git clean -fdq
CALL git clean -fdq
IF %ERRORLEVEL% NEQ 0 EXIT %ERRORLEVEL%

REM Determine if a GitHub pull request fetch is possible

SET PULL_REQUEST_FETCH=false
IF NOT "%BUILDKITE_PULL_REQUEST%" == "false" (
  IF "%BUILDKITE_PROJECT_PROVIDER%" == "github" SET PULL_REQUEST_FETCH=true
  IF "%BUILDKITE_PROJECT_PROVIDER%" == "github_enterprise" SET PULL_REQUEST_FETCH=true
)

if "%PULL_REQUEST_FETCH%" == "true" (
  REM Fetch the code using the special GitHub PR syntax

  ECHO ^> git fetch origin "+refs/pull/%BUILDKITE_PULL_REQUEST%/head:"
  CALL git fetch origin "+refs/pull/%BUILDKITE_PULL_REQUEST%/head:"
) ELSE (
  REM Fetch the latest code

  ECHO ^> git fetch -q
  CALL git fetch -q
  IF %ERRORLEVEL% NEQ 0 EXIT %ERRORLEVEL%

  REM Only reset to the branch if we're not on a tag

  IF "%BUILDKITE_TAG%" == "" (
    ECHO ^> git reset --hard origin/%BUILDKITE_BRANCH%
    CALL git reset --hard origin/%BUILDKITE_BRANCH%
    IF !ERRORLEVEL! NEQ 0 EXIT !ERRORLEVEL!
  )
)

ECHO ^> git checkout -qf "%BUILDKITE_COMMIT%"
CALL git checkout -qf "%BUILDKITE_COMMIT%"
IF %ERRORLEVEL% NEQ 0 EXIT %ERRORLEVEL%

ECHO ~~~ Running build script

IF "%BUILDKITE_SCRIPT_PATH%" == "" (
  echo ERROR: No script path has been set for this project. Please go to \"Project Settings\" and add the path to your build script
  exit 1
) ELSE (
  ECHO ^> CALL %BUILDKITE_SCRIPT_PATH%
  CALL %BUILDKITE_SCRIPT_PATH%
  SET EXIT_STATUS=!ERRORLEVEL!
)

IF NOT "%BUILDKITE_ARTIFACT_PATHS%" == "" (
  REM If you want to upload artifacts to your own server, uncomment the lines below
  REM and replace the AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY with keys to your
  REM own bucket.
  REM
  REM SET AWS_SECRET_ACCESS_KEY=yyy
  REM SET AWS_ACCESS_KEY_ID=xxx
  REM SET AWS_S3_ACL=private
  REM call buildkite-agent artifact upload "%BUILDKITE_ARTIFACT_PATHS%" "s3://name-of-your-s3-bucket/%BUILDKITE_JOB_ID%"

  REM Show the output of the artifact uploder when in debug mode
  IF "%BUILDKITE_AGENT_DEBUG%" == "true" (
    ECHO ~~~ Uploading Artifacts
    ECHO ^> buildkite-agent artifact upload "%BUILDKITE_ARTIFACT_PATHS%"
    call buildkite-agent artifact upload "%BUILDKITE_ARTIFACT_PATHS%"
    IF !ERRORLEVEL! NEQ 0 EXIT !ERRORLEVEL!
  ) ELSE (
    call buildkite-agent artifact upload "%BUILDKITE_ARTIFACT_PATHS%" > nul 2>&1
    IF !ERRORLEVEL! NEQ 0 EXIT !ERRORLEVEL!
  )
)

EXIT %EXIT_STATUS%
