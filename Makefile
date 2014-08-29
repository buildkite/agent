VERSION := $(shell cat buildbox/version.go  | grep Version | head -n1 | cut -d \" -f 2)

build: deps
	@echo "building ${VERSION}"
	@go build -o pkg/buildbox *.go

dist: deps
	@scripts/build.sh

deps:
	@godep get ./...

clean:
	@test ! -e pkg || rm -r pkg

test:
	@go test ./...

fmt:
	@go fmt ./...

lint:
	@golint cmd buildbox

vet:
	@go vet ./...

.PHONY: build dist clean test fmt vet
