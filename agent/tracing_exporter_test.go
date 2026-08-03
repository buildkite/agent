package agent

import (
	"bytes"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/tracetools"
)

func TestEncodeOTLPHeaders(t *testing.T) {
	t.Parallel()

	got := encodeOTLPHeaders(map[string]string{
		"X-Team":        "pipelines",
		"Authorization": "Bearer secret,with%chars",
		"":              "skip",
		"Empty":         "",
	})
	// Values are PathEscaped so commas/percent signs survive OTel's parser.
	want := "Authorization=Bearer%20secret%2Cwith%25chars,X-Team=pipelines"
	if got != want {
		t.Fatalf("encodeOTLPHeaders() = %q, want %q", got, want)
	}
}

func TestControlPlaneExporterApplies(t *testing.T) {
	exporter := &api.TracingExporter{Endpoint: "https://otel.example/v1/traces"}

	tests := []struct {
		name        string
		backend     string
		exporter    *api.TracingExporter
		hostEnv     map[string]string
		wantApplies bool
	}{
		{
			name:        "opentelemetry backend with exporter and no host config",
			backend:     tracetools.BackendOpenTelemetry,
			exporter:    exporter,
			wantApplies: true,
		},
		{
			name:        "no exporter supplied",
			backend:     tracetools.BackendOpenTelemetry,
			exporter:    nil,
			wantApplies: false,
		},
		{
			name:        "exporter without endpoint",
			backend:     tracetools.BackendOpenTelemetry,
			exporter:    &api.TracingExporter{},
			wantApplies: false,
		},
		{
			name:        "non-opentelemetry backend",
			backend:     tracetools.BackendDatadog,
			exporter:    exporter,
			wantApplies: false,
		},
		{
			name:        "tracing disabled",
			backend:     "",
			exporter:    exporter,
			wantApplies: false,
		},
		{
			name:        "host endpoint wins",
			backend:     tracetools.BackendOpenTelemetry,
			exporter:    exporter,
			hostEnv:     map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "https://local.example"},
			wantApplies: false,
		},
		{
			name:        "host traces headers win",
			backend:     tracetools.BackendOpenTelemetry,
			exporter:    exporter,
			hostEnv:     map[string]string{"OTEL_EXPORTER_OTLP_TRACES_HEADERS": "Authorization=Bearer local"},
			wantApplies: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, key := range env.OTelExporterKeys {
				t.Setenv(key, "")
			}
			for key, value := range test.hostEnv {
				t.Setenv(key, value)
			}

			if got := controlPlaneExporterApplies(test.backend, test.exporter); got != test.wantApplies {
				t.Fatalf("controlPlaneExporterApplies() = %v, want %v", got, test.wantApplies)
			}
		})
	}
}

// Without control-plane exporter config, a pipeline keeps whatever OTLP exporter
// env it sets, exactly as it does when this feature is unused.
func TestScrubJobOTelExporterEnvLeavesPipelineConfigWhenNotApplying(t *testing.T) {
	t.Parallel()

	jobEnv := map[string]string{
		"BUILDKITE_COMMAND":           "echo hi",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "https://our-own-collector.example",
		"OTEL_EXPORTER_OTLP_HEADERS":  "Authorization=Bearer ours",
	}
	scrubbed := scrubJobOTelExporterEnv(jobEnv, false)

	if len(scrubbed) != 0 {
		t.Fatalf("scrubbed = %v, want nothing removed", scrubbed)
	}
	if jobEnv["OTEL_EXPORTER_OTLP_ENDPOINT"] != "https://our-own-collector.example" {
		t.Fatalf("jobEnv = %#v, want pipeline exporter config kept", jobEnv)
	}
}

func TestScrubJobOTelExporterEnvWhenApplying(t *testing.T) {
	t.Parallel()

	jobEnv := map[string]string{
		"BUILDKITE_COMMAND":                     "echo hi",
		"OTEL_EXPORTER_OTLP_HEADERS":            "Authorization=Bearer pipeline",
		"OTEL_EXPORTER_OTLP_ENDPOINT":           "https://evil.example",
		"OTEL_EXPORTER_OTLP_PROTOCOL":           "grpc",
		"OTEL_EXPORTER_OTLP_TRACES_INSECURE":    "true",
		"OTEL_EXPORTER_OTLP_CERTIFICATE":        "/tmp/attacker-ca.pem",
		"OTEL_EXPORTER_OTLP_CLIENT_KEY":         "/tmp/key.pem",
		"OTEL_EXPORTER_OTLP_TRACES_COMPRESSION": "gzip",
		"OTEL_RESOURCE_ATTRIBUTES":              "keep=me",
	}
	scrubbed := scrubJobOTelExporterEnv(jobEnv, true)

	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_HEADERS",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_INSECURE",
		"OTEL_EXPORTER_OTLP_CERTIFICATE",
	} {
		if _, ok := jobEnv[key]; ok {
			t.Errorf("expected %s to be scrubbed", key)
		}
	}
	// Compression and resource attributes are neither destination, auth, nor TLS.
	if jobEnv["OTEL_EXPORTER_OTLP_TRACES_COMPRESSION"] != "gzip" {
		t.Error("expected compression to remain")
	}
	if jobEnv["OTEL_RESOURCE_ATTRIBUTES"] != "keep=me" {
		t.Error("expected non-exporter OTEL vars to remain")
	}
	if len(scrubbed) != 6 {
		t.Errorf("scrubbed = %v, want 6 exporter keys", scrubbed)
	}
}

// The marker drives removal in bootstrap, so a pipeline must never supply it.
func TestScrubJobOTelExporterEnvAlwaysDropsMarker(t *testing.T) {
	t.Parallel()

	for _, applies := range []bool{true, false} {
		jobEnv := map[string]string{env.OTelExporterMarkerEnv: "true"}
		scrubbed := scrubJobOTelExporterEnv(jobEnv, applies)

		if _, ok := jobEnv[env.OTelExporterMarkerEnv]; ok {
			t.Fatalf("exporterApplies=%v: expected the marker to be scrubbed", applies)
		}
		if len(scrubbed) != 1 {
			t.Fatalf("exporterApplies=%v: scrubbed = %v, want the marker", applies, scrubbed)
		}
	}
}

func TestOTelExporterMarkerIsProtected(t *testing.T) {
	t.Parallel()

	if !env.IsProtected(env.OTelExporterMarkerEnv) {
		t.Errorf("%s should be protected", env.OTelExporterMarkerEnv)
	}
	if !env.IsProtectedFromWithinJob(env.OTelExporterMarkerEnv) {
		t.Errorf("%s should be protected from within job", env.OTelExporterMarkerEnv)
	}
}

// Exporter env vars themselves stay unprotected: a job may configure its own
// OTLP export, and the agent's injected values are removed before hooks run.
func TestOTelExporterKeysAreNotGloballyProtected(t *testing.T) {
	t.Parallel()

	for _, key := range env.OTelExporterKeys {
		if env.IsProtected(key) {
			t.Errorf("%s should not be globally protected", key)
		}
	}
}

func TestApplyRegistrationTracing(t *testing.T) {
	t.Parallel()

	t.Run("applies backend, propagate, and exporter", func(t *testing.T) {
		t.Parallel()
		conf := AgentConfiguration{}
		conf.ApplyRegistrationTracing(logger.Discard, &api.AgentRegistrationTracing{
			Backend:              tracetools.BackendOpenTelemetry,
			PropagateTraceparent: true,
			Exporter: &api.TracingExporter{
				Endpoint: "https://otel.example/v1/traces",
				Protocol: "http/protobuf",
				Headers:  map[string]string{"Authorization": "Bearer secret"},
			},
		}, false, false)

		if conf.TracingBackend != tracetools.BackendOpenTelemetry {
			t.Fatalf("TracingBackend = %q", conf.TracingBackend)
		}
		if !conf.TracingPropagateTraceparent {
			t.Fatal("TracingPropagateTraceparent = false")
		}
		if conf.TracingExporter == nil || conf.TracingExporter.Endpoint != "https://otel.example/v1/traces" {
			t.Fatalf("TracingExporter = %#v", conf.TracingExporter)
		}
	})

	t.Run("local backend wins", func(t *testing.T) {
		t.Parallel()
		conf := AgentConfiguration{TracingBackend: tracetools.BackendDatadog}
		conf.ApplyRegistrationTracing(logger.Discard, &api.AgentRegistrationTracing{
			Backend: tracetools.BackendOpenTelemetry,
			Exporter: &api.TracingExporter{
				Endpoint: "https://otel.example/v1/traces",
			},
		}, true, false)

		if conf.TracingBackend != tracetools.BackendDatadog {
			t.Fatalf("TracingBackend = %q", conf.TracingBackend)
		}
		if conf.TracingExporter != nil {
			t.Fatalf("TracingExporter = %#v, want nil", conf.TracingExporter)
		}
	})

	t.Run("local propagate wins including explicit false", func(t *testing.T) {
		t.Parallel()
		conf := AgentConfiguration{TracingPropagateTraceparent: false}
		conf.ApplyRegistrationTracing(logger.Discard, &api.AgentRegistrationTracing{
			Backend:              tracetools.BackendOpenTelemetry,
			PropagateTraceparent: true,
			Exporter: &api.TracingExporter{
				Endpoint: "https://otel.example/v1/traces",
			},
		}, false, true)

		if conf.TracingPropagateTraceparent {
			t.Fatal("registration overwrote an explicit local propagate-traceparent=false")
		}
		if conf.TracingExporter == nil {
			t.Fatal("TracingExporter = nil")
		}
	})

	t.Run("ignores non-opentelemetry registration backend", func(t *testing.T) {
		t.Parallel()
		conf := AgentConfiguration{}
		conf.ApplyRegistrationTracing(logger.Discard, &api.AgentRegistrationTracing{
			Backend: tracetools.BackendDatadog,
			Exporter: &api.TracingExporter{
				Endpoint: "https://otel.example/v1/traces",
			},
		}, false, false)
		if conf.TracingBackend != "" || conf.TracingExporter != nil {
			t.Fatalf("unexpected apply: %#v", conf)
		}
	})

	t.Run("ignores exporter without endpoint", func(t *testing.T) {
		t.Parallel()
		conf := AgentConfiguration{}
		conf.ApplyRegistrationTracing(logger.Discard, &api.AgentRegistrationTracing{
			Backend:  tracetools.BackendOpenTelemetry,
			Exporter: &api.TracingExporter{},
		}, false, false)
		if conf.TracingExporter != nil {
			t.Fatalf("unexpected exporter accept: %#v", conf)
		}
	})
}

// Registration can enable tracing on an agent that configured none, so the agent
// log has to say what was accepted and where traces are going.
func TestApplyRegistrationTracingLogging(t *testing.T) {
	fullExporter := &api.TracingExporter{
		// A path a vendor might use to carry an ingest key.
		Endpoint: "https://otel.example/ingest/SECRET_KEY/v1/traces",
		Protocol: "http/protobuf",
		Headers:  map[string]string{"Authorization": "Bearer SECRET_HEADER"},
	}

	tests := []struct {
		name       string
		conf       AgentConfiguration
		tracing    *api.AgentRegistrationTracing
		backendSet bool
		hostEnv    map[string]string
		wantLogged []string
		wantSilent bool
	}{
		{
			name: "enabled with an exporter",
			tracing: &api.AgentRegistrationTracing{
				Backend:  tracetools.BackendOpenTelemetry,
				Exporter: fullExporter,
			},
			wantLogged: []string{
				"Buildkite enabled OpenTelemetry tracing for this agent",
				"Exporting job traces to https://otel.example over http/protobuf, with request headers supplied by Buildkite",
			},
		},
		{
			name: "exporter without headers",
			tracing: &api.AgentRegistrationTracing{
				Backend:  tracetools.BackendOpenTelemetry,
				Exporter: &api.TracingExporter{Endpoint: "https://otel.example/v1/traces"},
			},
			wantLogged: []string{
				"Exporting job traces to https://otel.example over http/protobuf, with no request headers",
			},
		},
		{
			name:       "local backend wins",
			conf:       AgentConfiguration{TracingBackend: tracetools.BackendDatadog},
			backendSet: true,
			tracing: &api.AgentRegistrationTracing{
				Backend:  tracetools.BackendOpenTelemetry,
				Exporter: fullExporter,
			},
			wantLogged: []string{`Ignoring it, because the tracing backend is set to "datadog" locally`},
		},
		{
			name: "host exporter config wins",
			tracing: &api.AgentRegistrationTracing{
				Backend:  tracetools.BackendOpenTelemetry,
				Exporter: fullExporter,
			},
			hostEnv:    map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "https://operator-collector.example"},
			wantLogged: []string{"Ignoring it, because this host sets OTEL_EXPORTER_OTLP_* itself"},
		},
		{
			name:       "nothing supplied",
			tracing:    nil,
			wantSilent: true,
		},
		{
			name:       "non-opentelemetry registration backend",
			tracing:    &api.AgentRegistrationTracing{Backend: tracetools.BackendDatadog},
			wantSilent: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, key := range env.OTelExporterKeys {
				t.Setenv(key, "")
			}
			for key, value := range test.hostEnv {
				t.Setenv(key, value)
			}

			buf := logger.NewBuffer()
			conf := test.conf
			conf.ApplyRegistrationTracing(buf, test.tracing, test.backendSet, false)

			if test.wantSilent && len(buf.Messages) != 0 {
				t.Fatalf("expected no log output, got %v", buf.Messages)
			}
			logged := strings.Join(buf.Messages, "\n")
			for _, want := range test.wantLogged {
				if !strings.Contains(logged, want) {
					t.Errorf("log missing %q, got:\n%s", want, logged)
				}
			}
			// Neither the credential nor a path that might carry one is loggable.
			for _, secret := range []string{"SECRET_HEADER", "SECRET_KEY"} {
				if strings.Contains(logged, secret) {
					t.Errorf("log leaked %s:\n%s", secret, logged)
				}
			}
		})
	}
}

// These lines exist to be seen without extra flags, so they go through a real
// level-aware ConsoleLogger here rather than logger.Buffer, which ignores levels.
//
// Note the level ordering in logger/level.go: NOTICE sits *below* INFO, unlike
// syslog. Infof prints when level <= INFO, so it is visible at the `start`
// default of notice as well as at info and debug. Noticef would print when
// level <= NOTICE, which would hide these lines from anyone running the more
// verbose-sounding --log-level info.
func TestApplyRegistrationTracingLogsAtDefaultLogLevel(t *testing.T) {
	levels := map[string]struct {
		level       logger.Level
		wantVisible bool
	}{
		"debug":                      {logger.DEBUG, true},
		"notice (the start default)": {logger.NOTICE, true},
		"info":                       {logger.INFO, true},
		"warn":                       {logger.WARN, false},
	}

	for name, test := range levels {
		t.Run(name, func(t *testing.T) {
			for _, key := range env.OTelExporterKeys {
				t.Setenv(key, "")
			}

			var buf bytes.Buffer
			l := logger.NewConsoleLogger(logger.NewTextPrinter(&buf), func(int) {})
			l.SetLevel(test.level)

			conf := AgentConfiguration{}
			conf.ApplyRegistrationTracing(l, &api.AgentRegistrationTracing{
				Backend: tracetools.BackendOpenTelemetry,
				Exporter: &api.TracingExporter{
					Endpoint: "https://otel.example/v1/traces",
					Headers:  map[string]string{"Authorization": "Bearer secret"},
				},
			}, false, false)

			got := buf.String()
			visible := strings.Contains(got, "Buildkite enabled OpenTelemetry tracing") &&
				strings.Contains(got, "Exporting job traces to https://otel.example")
			if visible != test.wantVisible {
				t.Fatalf("visible at %s = %v, want %v; output:\n%s", test.level, visible, test.wantVisible, got)
			}
		})
	}
}

func TestExporterEndpointForLog(t *testing.T) {
	t.Parallel()

	for endpoint, want := range map[string]string{
		"https://otel.example/v1/traces":           "https://otel.example",
		"https://otel.example:4318/ingest/key?a=b": "https://otel.example:4318",
		"http://localhost:4318":                    "http://localhost:4318",
		"not a url":                                "an unparseable endpoint",
		"":                                         "an unparseable endpoint",
	} {
		if got := exporterEndpointForLog(endpoint); got != want {
			t.Errorf("exporterEndpointForLog(%q) = %q, want %q", endpoint, got, want)
		}
	}
}

func TestApplyControlPlaneTracingExporter(t *testing.T) {
	t.Parallel()

	got := map[string]string{}
	setEnv := func(name, value string) { got[name] = value }

	applyControlPlaneTracingExporter(setEnv, &api.TracingExporter{
		Endpoint: "https://otel.example/v1/traces",
		Protocol: "http/protobuf",
		Headers:  map[string]string{"Authorization": "Bearer from-cp"},
	})

	want := map[string]string{
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "https://otel.example/v1/traces",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/protobuf",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS":  "Authorization=Bearer%20from-cp",
		env.OTelExporterMarkerEnv:            "true",
	}
	for name, wantValue := range want {
		if got[name] != wantValue {
			t.Errorf("%s = %q, want %q", name, got[name], wantValue)
		}
	}

	// Generic variables are shared with the logs and metrics SDKs, which honour
	// their own signal-specific endpoint. Setting them would let a pipeline-set
	// OTEL_EXPORTER_OTLP_LOGS_ENDPOINT receive the registration credential.
	if len(got) != len(want) {
		t.Errorf("got %#v, want only traces-scoped variables plus the marker", got)
	}
}

func TestApplyControlPlaneTracingExporterDefaultsProtocol(t *testing.T) {
	t.Parallel()

	got := map[string]string{}
	setEnv := func(name, value string) { got[name] = value }

	applyControlPlaneTracingExporter(setEnv, &api.TracingExporter{
		Endpoint: "https://otel.example/v1/traces",
	})

	if got["OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"] != "http/protobuf" {
		t.Errorf("protocol = %q, want http/protobuf", got["OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"])
	}
	if _, ok := got["OTEL_EXPORTER_OTLP_TRACES_HEADERS"]; ok {
		t.Error("expected no headers variable when the exporter supplies none")
	}
}
