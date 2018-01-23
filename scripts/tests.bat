@echo off
if not exist ".\tmp" mkdir .\tmp

SET GOBIN=C:\gopath\bin
SET "PATH=%GOBIN%;%PATH%"

echo ~~~ Installing test dependencies
go get github.com/kyoh86/richgo
go get github.com/jstemmer/go-junit-report

echo +++ Running tests
go test -race ./... 2>&1 | richgo testfilter

if %errorlevel% neq 0 exit /b %errorlevel%
