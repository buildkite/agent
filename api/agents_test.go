package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
)

func TestRegisterIndependentInternalTracingExporter(t *testing.T) {
	t.Parallel()
	for _, customer := range []bool{false, true} {
		t.Run(fmt.Sprint(customer), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := map[string]any{
					"id": "agent-1",
					"internal_tracing_exporter": api.TracingExporter{
						Endpoint: "https://internal.example/v1/traces", Protocol: "http/protobuf",
						Headers: map[string]string{"Authorization": "Bearer internal-secret"},
					},
				}
				if customer {
					response["tracing"] = api.AgentTracing{Backend: "opentelemetry", Exporter: &api.TracingExporter{Endpoint: "https://customer.example/v1/traces"}}
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			logs := logger.NewBuffer()
			client := api.NewClient(logs, api.Config{Endpoint: server.URL, Token: "llamas", DebugHTTP: true})
			reg, _, err := client.Register(t.Context(), &api.AgentRegisterRequest{Features: []string{"internal-otlp-tracing"}})
			if err != nil {
				t.Fatal(err)
			}
			if exp := reg.InternalTracingExporter; exp == nil || exp.Endpoint != "https://internal.example/v1/traces" || exp.Protocol != "http/protobuf" || exp.Headers["Authorization"] != "Bearer internal-secret" {
				t.Errorf("registration lost internal exporter: %+v", exp)
			}
			if (reg.Tracing != nil) != customer {
				t.Errorf("customer tracing present = %t, want %t", reg.Tracing != nil, customer)
			}
			for _, message := range logs.Messages {
				if strings.Contains(message, "internal-secret") || strings.Contains(message, "internal.example") {
					t.Errorf("registration logged internal collector configuration: %s", message)
				}
			}
		})
	}
}

func TestRegisterMalformedResponseDoesNotExposeInternalExporter(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, `{"internal_tracing_exporter":{"endpoint":"https://internal.example/v1/traces","headers":{"Authorization":"Bearer internal-secret"}}`)
			}))
			defer server.Close()
			client := api.NewClient(logger.Discard, api.Config{Endpoint: server.URL, Token: "llamas"})
			_, _, err := client.Register(t.Context(), &api.AgentRegisterRequest{})
			if err == nil {
				t.Fatal("Register accepted malformed JSON")
			}
			if message := err.Error(); strings.Contains(message, "internal-secret") || strings.Contains(message, "internal.example") {
				t.Errorf("registration error exposes internal configuration: %s", message)
			}
		})
	}
}
