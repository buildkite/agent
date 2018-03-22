## Development docker image for buildkite-agent
FROM golang:1.10
WORKDIR /go/src/github.com/buildkite/agent
COPY . .
RUN go build -i -o /go/bin/buildkite-agent github.com/buildkite/agent
CMD ["buildkite-agent", "start"]
