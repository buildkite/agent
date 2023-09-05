package status

import (
	"context"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/version"
)

func TestSmokeErrorTemplate(t *testing.T) {
	errData := &errorData{
		Operation: "Couldn't fluff the llamas",
		Error:     errors.New("llama comb unavailable"),
		Item: map[string]any{
			"llamas":  "yes",
			"alpacas": 42,
		},
	}

	if err := errorTmpl.Execute(io.Discard, errData); err != nil {
		t.Errorf("errorTmpl.Execute(io.Discard, errData) = %v", err)
	}
}

func TestSmokeStatusTemplate(t *testing.T) {
	data := &statusData{
		Items: map[string]item{
			"Llamas": &simpleItem{
				stat: "✅ Llamas enabled",
			},
			"Alpacas": &templatedItem{
				tmpl: template.Must(template.New("alpacas").Parse("Alpacas enabled at: {{.AlpacasEnabled}}")),
				cb: func(context.Context) (any, error) {
					return struct {
						AlpacasEnabled time.Time
					}{time.Now()}, nil
				},
			},
		},
		Version:      version.Version(),
		Build:        version.BuildNumber(),
		Hostname:     hostname,
		Username:     username,
		ExePath:      exepath,
		PID:          os.Getpid(),
		Compiler:     runtime.Compiler,
		RuntimeVer:   runtime.Version(),
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		StartTime:    startTime.Format(time.RFC1123),
		StartTimeAgo: time.Since(startTime),
		CurrentTime:  time.Now().Format(time.RFC1123),
		Ctx:          context.Background(),
	}

	if err := statusTmpl.Execute(io.Discard, data); err != nil {
		t.Errorf("statusData.Execute(io.Discard, data) = %v", err)
	}
}

func TestSmokeHandle(t *testing.T) {
	ctx := context.Background()
	cctx, setStat, done := AddSimpleItem(ctx, "Llamas")
	defer done()
	setStat("Essence of Llama")

	_, setStat2, done2 := AddSimpleItem(cctx, "Kuzco")
	defer done2()
	setStat2("Oh, right. The poison. The poison for Kuzco, the poison chosen especially to kill Kuzco, Kuzco's poison.")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/status", nil)
	if err != nil {
		t.Fatalf("http.NewReqeustWithContext(GET /status) error = %v", err)
	}
	rec := httptest.NewRecorder()
	Handle(rec, req)
	if got, want := rec.Result().StatusCode, http.StatusOK; got != want {
		t.Errorf("Handle(rec, req): rec.Result().StatusCode = %v, want %v", got, want)
	}
}
