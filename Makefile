VERSION := $(shell cat buildbox/version.go  | grep Version | head -n1 | cut -d \" -f 2)

build: deps
	@echo "building ${VERSION}"
	@go build -o pkg/buildbox-agent cmd/agent/agent.go
	@go build -o pkg/buildbox-artifact cmd/artifact/artifact.go
	@go build -o pkg/buildbox-data cmd/data/data.go

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

.PHONY: build clean test fmt vet
