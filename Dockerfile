FROM golang:1.8

WORKDIR /go/src/github.com/buildkite/agent
COPY . .

RUN go build -o buildkite-agent *.go

ENTRYPOINT ["./buildkite-agent"]