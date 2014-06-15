deps: build
	@godep get ./...

build:
	@scripts/build.sh

clean:
	@test ! -e pkg || rm -r pkg

test:
	@go test ./...

fmt:
	@go fmt ./...

vet:
	@go vet ./...

.PHONY: build clean test fmt vet
