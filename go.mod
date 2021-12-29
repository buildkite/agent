module github.com/buildkite/agent/v3

go 1.14

require (
	cloud.google.com/go v0.34.0
	github.com/DataDog/datadog-go v3.7.2+incompatible
	github.com/aws/aws-sdk-go v1.32.10
	github.com/buildkite/bintest/v3 v3.1.0
	github.com/buildkite/interpolate v0.0.0-20200526001904-07f35b4ae251
	github.com/buildkite/shellwords v0.0.0-20180315084142-c3f497d1e000
	github.com/buildkite/yaml v0.0.0-20210326113714-4a3f40911396
	github.com/creack/pty v1.1.12
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/denisbrodbeck/machineid v1.0.0
	github.com/google/go-querystring v0.0.0-20170111101155-53e6ce116135
	github.com/mattn/go-zglob v0.0.0-20180803001819-2ea3427bfa53
	github.com/mitchellh/go-homedir v1.0.0
	github.com/nightlyone/lockfile v0.0.0-20180618180623-0ad87eef1443
	github.com/oleiade/reflections v0.0.0-20160817071559-0e86b3c98b2f
	github.com/pborman/uuid v0.0.0-20170112150404-1b00554d8222
	github.com/pkg/errors v0.9.1
	github.com/qri-io/jsonpointer v0.0.0-20180309164927-168dd9e45cf2 // indirect
	github.com/qri-io/jsonschema v0.0.0-20180607150648-d0d3b10ec792
	github.com/rjeczalik/interfaces v0.1.1
	github.com/sergi/go-diff v1.0.0 // indirect
	github.com/stretchr/testify v1.7.0
	github.com/urfave/cli v1.22.4
	go.opentelemetry.io/contrib/propagators/aws v1.3.0
	go.opentelemetry.io/contrib/propagators/b3 v1.3.0
	go.opentelemetry.io/contrib/propagators/jaeger v1.3.0
	go.opentelemetry.io/contrib/propagators/ot v1.3.0
	go.opentelemetry.io/otel v1.3.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.3.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.3.0
	go.opentelemetry.io/otel/sdk v1.3.0
	go.opentelemetry.io/otel/trace v1.3.0
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	golang.org/x/sys v0.0.0-20210510120138-977fb7262007
	google.golang.org/api v0.0.0-20181016191922-cc9bd73d51b4
)
