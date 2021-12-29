# Tracing Demo Setup

The directory contains sample [OpenTelemetry Collector](https://github.com/open-telemetry/opentelemetry-collector)
and [Jaeger](https://www.jaegertracing.io) configurations for a tracing demo.

## Configuration

The provided [docker-compose.yaml](docker-compose.yaml) sets up 2 Containers

1. OpenTelemetry Collector listening on port 4317 for GRPC
2. Jaeger all-in-one listening on multiple ports

The collector forwards all received spans to Jaeger over port 14250 and Jaeger exposes a UI over port `16686`.

## Usage

1. Start all the Containers
```shell
make run
```

2. Export

```shell
export OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://localhost:4317

buildkite-agent --tracing-backend otel --build-path build
```

3. Visit `http://localhost:16686/` to see the spans

4. Stop all the containers
```shell
make stop
```
