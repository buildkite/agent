module github.com/buildkite/agent/v3

go 1.20

require (
	cloud.google.com/go/compute/metadata v0.2.3
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.4.0
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v1.2.0
	github.com/DataDog/datadog-go/v5 v5.3.0
	github.com/DrJosh9000/zzglob v0.0.16
	github.com/aws/aws-sdk-go v1.46.1
	github.com/brunoscheufler/aws-ecs-metadata-go v0.0.0-20220812150832-b6b31c6eeeaf
	github.com/buildkite/bintest/v3 v3.1.1
	github.com/buildkite/interpolate v0.0.0-20200526001904-07f35b4ae251
	github.com/buildkite/roko v1.1.1
	github.com/buildkite/shellwords v0.0.0-20180315084142-c3f497d1e000
	github.com/creack/pty v1.1.18
	github.com/denisbrodbeck/machineid v1.0.1
	github.com/dustin/go-humanize v1.0.1
	github.com/gliderlabs/ssh v0.3.5
	github.com/go-chi/chi/v5 v5.0.10
	github.com/gofrs/flock v0.8.1
	github.com/google/go-cmp v0.6.0
	github.com/google/go-querystring v1.1.0
	github.com/lestrrat-go/jwx/v2 v2.0.13
	github.com/mattn/go-zglob v0.0.4
	github.com/mitchellh/go-homedir v1.1.0
	github.com/oleiade/reflections v1.0.1
	github.com/opentracing/opentracing-go v1.2.0
	github.com/pborman/uuid v1.2.1
	github.com/puzpuzpuz/xsync/v2 v2.5.1
	github.com/qri-io/jsonschema v0.2.1
	github.com/stretchr/testify v1.8.4
	github.com/urfave/cli v1.22.14
	go.opentelemetry.io/contrib/propagators/aws v1.20.0
	go.opentelemetry.io/contrib/propagators/b3 v1.20.0
	go.opentelemetry.io/contrib/propagators/jaeger v1.20.0
	go.opentelemetry.io/contrib/propagators/ot v1.20.0
	go.opentelemetry.io/otel v1.19.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.19.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.19.0
	go.opentelemetry.io/otel/sdk v1.19.0
	go.opentelemetry.io/otel/trace v1.19.0
	golang.org/x/crypto v0.14.0
	golang.org/x/exp v0.0.0-20230321023759-10a507213a29
	golang.org/x/oauth2 v0.13.0
	golang.org/x/sys v0.13.0
	google.golang.org/api v0.148.0
	gopkg.in/DataDog/dd-trace-go.v1 v1.55.0
	gopkg.in/yaml.v3 v3.0.1
	gotest.tools/v3 v3.5.1
)

require github.com/dustinkirkland/golang-petname v0.0.0-20231002161417-6a283f1aaaf2

require (
	cloud.google.com/go/compute v1.23.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.8.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.3.0 // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.1.1 // indirect
	github.com/DataDog/appsec-internal-go v1.0.0 // indirect
	github.com/DataDog/datadog-agent/pkg/obfuscate v0.46.0 // indirect
	github.com/DataDog/datadog-agent/pkg/remoteconfig/state v0.48.0-devel.0.20230725154044-2549ba9058df // indirect
	github.com/DataDog/go-libddwaf v1.5.0 // indirect
	github.com/DataDog/go-tuf v1.0.2-0.5.2 // indirect
	github.com/DataDog/sketches-go v1.4.2 // indirect
	github.com/Microsoft/go-winio v0.6.1 // indirect
	github.com/anmitsu/go-shlex v0.0.0-20200514113438-38f4b401e2be // indirect
	github.com/cenkalti/backoff/v4 v4.2.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.2.0 // indirect
	github.com/ebitengine/purego v0.5.0-alpha.1 // indirect
	github.com/go-logr/logr v1.2.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.0.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/s2a-go v0.1.7 // indirect
	github.com/google/uuid v1.3.1 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.1 // indirect
	github.com/googleapis/gax-go/v2 v2.12.0 // indirect
	github.com/gowebpki/jcs v1.0.1
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.16.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/lestrrat-go/blackmagic v1.0.2 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/httprc v1.0.4 // indirect
	github.com/lestrrat-go/iter v1.0.2 // indirect
	github.com/lestrrat-go/option v1.0.1 // indirect
	github.com/outcaste-io/ristretto v0.2.3 // indirect
	github.com/petermattis/goid v0.0.0-20180202154549-b0b1615b78e5 // indirect
	github.com/philhofer/fwd v1.1.2 // indirect
	github.com/pkg/browser v0.0.0-20210911075715-681adbf594b8 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/qri-io/jsonpointer v0.1.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sasha-s/go-deadlock v0.0.0-20180226215254-237a9547c8a5 // indirect
	github.com/secure-systems-lab/go-securesystemslib v0.7.0 // indirect
	github.com/segmentio/asm v1.2.0 // indirect
	github.com/tinylib/msgp v1.1.8 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/otel/metric v1.19.0 // indirect
	go.opentelemetry.io/proto/otlp v1.0.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go4.org/intern v0.0.0-20230525184215-6c62f75575cb // indirect
	go4.org/unsafe/assume-no-moving-gc v0.0.0-20230525183740-e7c30c78aeb2 // indirect
	golang.org/x/mod v0.12.0 // indirect
	golang.org/x/net v0.17.0 // indirect
	golang.org/x/term v0.13.0 // indirect
	golang.org/x/text v0.13.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	golang.org/x/tools v0.12.1-0.20230815132531-74c255bcf846 // indirect
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20231002182017-d307bd883b97 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20231012201019-e917dd12ba7a // indirect
	google.golang.org/grpc v1.58.3 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
	inet.af/netaddr v0.0.0-20230525184311-b8eac61e914a // indirect
)
