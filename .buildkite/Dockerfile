FROM golang:1.12

ENV GOFLAGS=-mod=vendor \
    GO111MODULE=off

WORKDIR /go/src/github.com/buildkite/agent

# prime the object cache
ADD . .
RUN go install -i github.com/buildkite/agent
