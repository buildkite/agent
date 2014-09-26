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

echo --- Running %BUILDBOX_SCRIPT_PATH%

IF "%BUILDBOX_SCRIPT_PATH%" == "" (
  echo ERROR: No script path has been set for this project. Please go to \"Project Settings\" and add the path to your build script
  exit 1
) ELSE (
  CALL %BUILDBOX_SCRIPT_PATH%
)
