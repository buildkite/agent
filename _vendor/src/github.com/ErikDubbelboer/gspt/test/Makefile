
all: test


test: test.go
	GOPATH=`pwd`/deps ./go/bin/go build $^


clean:
	GOPATH=`pwd`/deps ./go/bin/go clean

distclean: clean
	rm -rf deps/pkg/*

go/bin/go:
	curl -O "http://go.googlecode.com/files/go1.1.2.src.tar.gz"
	tar -xzf go1.1.2.src.tar.gz
	rm go1.1.2.src.tar.gz
	cd go/src && ./all.bash

deps: go/bin/go
	GOPATH=`pwd`/deps ./go/bin/go get github.com/ErikDubbelboer/gspt


.PHONY: all clean distclean deps

