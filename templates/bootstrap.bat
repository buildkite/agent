@echo off

echo Current environment variables
SET

echo Current directory
chdir

REM You can remove this conditional once you've added a repository
IF "%BUILDBOX_REPO%" == "" (
  echo Congratulations! You just ran a build! Now it's time to fill out this build script and customize your project
  exit /b 0
)

REM Here are some basic setup instructions for checking out a git repository.
REM You have to manage checking out and updating the repo yourself.

IF NOT EXIST ".git" (
  echo Cloning %BUILDBOX_REPO%
  call git clone "%BUILDBOX_REPO%" .
  if %errorlevel% neq 0 exit /b %errorlevel%
)

echo Cleaning the repo
call git clean -fd
if %errorlevel% neq 0 exit /b %errorlevel%

echo Fetching latest commits from origin
call git fetch
if %errorlevel% neq 0 exit /b %errorlevel%

echo Checking out %BUILDBOX_COMMIT%
call git checkout -f "%BUILDBOX_COMMIT%"
if %errorlevel% neq 0 exit /b %errorlevel%

REM Here is an example on how to run tests for a Ruby on Rails project with rspec.
REM You can change these commands to be what ever you like.

echo Bundling
call bundle install
if %errorlevel% neq 0 exit /b %errorlevel%

REM bundle exec rake db:schema:load
echo Running specs
call bundle exec rspec
if %errorlevel% neq 0 exit /b %errorlevel%

REM Want to do something paticular for a specific branch? You can do that!
IF "%BUILDBOX_BRANCH%" == "master" (
  REM Do something special if the current branch is 'master'
)
