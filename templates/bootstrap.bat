@echo off

REM echo --- Environment Variables
REM SET

echo --- Creating Build Environment

REM Returns the location of this file

SET BUILDBOX_DIR=%~dp0

REM Add the BUILDBOX_DIR to the PATH

SET PATH=%PATH%;%BUILDBOX_DIR%

REM Create the build directory

SET SANITIED_PROJECT_SLUG=%BUILDBOX_PROJECT_SLUG:/=\%
SET BUILDBOX_BUILD_DIR=%BUILDBOX_DIR%%BUILDBOX_AGENT_NAME%\%SANITIED_PROJECT_SLUG%

IF NOT EXIST %BUILDBOX_BUILD_DIR% (
  REM Create the build directory

  ECHO ^> MKDIR %BUILDBOX_BUILD_DIR%
  MKDIR %BUILDBOX_BUILD_DIR%
  IF %ERRORLEVEL% NEQ 0 EXIT %ERRORLEVEL%
)

REM Move to the build directory

ECHO ^> CD %BUILDBOX_BUILD_DIR%
CD %BUILDBOX_BUILD_DIR%
IF %ERRORLEVEL% NEQ 0 EXIT %ERRORLEVEL%

REM Do we need to do a git checkout?

IF NOT EXIST ".git" (
  ECHO ^> git clone %BUILDBOX_REPO%
  CALL git clone "%BUILDBOX_REPO%" . -v
  ECHO It was this: %ERRORLEVEL%
  IF %ERRORLEVEL% NEQ 0 EXIT %ERRORLEVEL%
)

REM Clean the repo

ECHO ^> git clean -fdq
CALL git clean -fdq
IF %ERRORLEVEL% NEQ 0 EXIT %ERRORLEVEL%

REM Fetch the latest code

ECHO ^> git fetch -q
CALL git fetch -q
IF %ERRORLEVEL% NEQ 0 EXIT %ERRORLEVEL%

REM Only reset to the branch if we're not on a tag

IF "%BUILDBOX_TAG%" == "" (
  ECHO ^> git reset --hard origin/%BUILDBOX_BRANCH%
  CALL git reset --hard origin/%BUILDBOX_BRANCH%
  IF %ERRORLEVEL% NEQ 0 EXIT %ERRORLEVEL%
)

ECHO ^> git checkout -qf "%BUILDBOX_COMMIT%"
CALL git checkout -qf "%BUILDBOX_COMMIT%"
IF %ERRORLEVEL% NEQ 0 EXIT %ERRORLEVEL%

ECHO --- Running Build Script

IF "%BUILDBOX_SCRIPT_PATH%" == "" (
  echo ERROR: No script path has been set for this project. Please go to \"Project Settings\" and add the path to your build script
  exit 1
) ELSE (
  ECHO ^> CALL %BUILDBOX_SCRIPT_PATH%
  CALL %BUILDBOX_SCRIPT_PATH%
  SET EXIT_STATUS=%ERRORLEVEL%
)

IF NOT "%BUILDBOX_ARTIFACT_PATHS%" == "" (
  REM If you want to upload artifacts to your own server, uncomment the lines below
  REM and replace the AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY with keys to your
  REM own bucket.
  REM
  REM SET AWS_SECRET_ACCESS_KEY=yyy
  REM SET AWS_ACCESS_KEY_ID=xxx
  REM SET AWS_S3_ACL=private
  REM call buildbox-agent artifact upload "%BUILDBOX_ARTIFACT_PATHS%" "s3://name-of-your-s3-bucket/%BUILDBOX_JOB_ID%"

  REM Show the output of the artifact uploder when in debug mode
  IF "%BUILDBOX_AGENT_DEBUG%" == "true" (
    ECHO --- Uploading Artifacts
    ECHO ^> %BUILDBOX_DIR%\buildbox-agent artifact upload "%BUILDBOX_ARTIFACT_PATHS%"
    call %BUILDBOX_DIR%\buildbox-agent artifact upload "%BUILDBOX_ARTIFACT_PATHS%"
    IF %ERRORLEVEL% NEQ 0 EXIT %ERRORLEVEL%
  ) ELSE (
    call %BUILDBOX_DIR%\buildbox-agent artifact upload "%BUILDBOX_ARTIFACT_PATHS%" > nul 2>&1
    IF %ERRORLEVEL% NEQ 0 EXIT %ERRORLEVEL%
  )
)

EXIT %EXIT_STATUS%
