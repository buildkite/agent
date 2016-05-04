gspt
====

`setproctitle()` package for Go.

[![Build Status](https://travis-ci.org/ErikDubbelboer/gspt.png?branch=master)](https://travis-ci.org/ErikDubbelboer/gspt)

--------------------------------

Installation
------------

Simple install the package to your [$GOPATH](http://code.google.com/p/go-wiki/wiki/GOPATH "GOPATH") with the [go tool](http://golang.org/cmd/go/ "go command") from shell:
```bash
go get github.com/ErikDubbelboer/gspt
```
Make sure [Git is installed](http://git-scm.com/downloads) on your machine and in your system's `PATH`.

Usage
-----

```go
import "github.com/ErikDubbelboer/gspt"

gspt.SetProcTitle("some title")
```

Please check the [documentation](http://godoc.org/github.com/ErikDubbelboer/gspt) for more details.
