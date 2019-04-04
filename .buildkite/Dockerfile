FROM golang:1.12

WORKDIR /go/src/github.com/buildkite/agent

# prime the object cache
ADD . .
RUN go install github.com/buildkite/agent
