package socket

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type yesNoJSONServer struct{}

func (yesNoJSONServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}\n`, http.StatusMethodNotAllowed)
		return
	}
	switch r.URL.Path {
	case "/yes":
		w.Write([]byte(`{"message":"Yes!"}\n`))   //nolint:errcheck // test handler
	case "/no":
		w.Write([]byte(`{"message":"No."}\n`))     //nolint:errcheck // test handler
	case "/secret":
		if r.Header.Get("Authorization") != "Bearer llama" {
			http.Error(w, `{"error":"invalid authorization token"}\n`, http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"message":"seekruts"}\n`)) //nolint:errcheck // test handler
	default:
		http.Error(w, `{"error":"not found"}\n`, http.StatusNotFound)
	}
}

type messageResponse struct {
	Message string `json:"message"`
}

func TestClientDo(t *testing.T) {
	t.Parallel()

	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	sockPath := testSocketPath()
	svr, err := NewServer(sockPath, yesNoJSONServer{})
	if err != nil {
		t.Fatalf("NewServer(%q, yesNoJSONServer) = error %v", sockPath, err)
	}

	if err := svr.Start(); err != nil {
		t.Fatalf("srv.Start() = %v", err)
	}
	t.Cleanup(func() { svr.Close() }) //nolint:errcheck // best-effort cleanup in test

	cli, err := NewClient(ctx, sockPath, "llama")
	if err != nil {
		t.Fatalf("NewClient(ctx, %q, llama) = error %v", sockPath, err)
	}

	tests := []struct {
		method, url string
		want        messageResponse
	}{
		{
			method: http.MethodGet,
			url:    "http://yn/yes",
			want:   messageResponse{Message: "Yes!"},
		},
		{
			method: http.MethodGet,
			url:    "http://yn/no",
			want:   messageResponse{Message: "No."},
		},
		{
			method: http.MethodGet,
			url:    "http://yn/secret",
			want:   messageResponse{Message: "seekruts"},
		},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.url, func(t *testing.T) {
			t.Parallel()
			var got messageResponse
			if err := cli.Do(ctx, test.method, test.url, nil, &got); err != nil {
				t.Errorf("cli.Do(ctx, %q, %q, nil, &got) = %v", test.method, test.url, err)
			}
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("response message diff (-got +want)\n%s", diff)
			}
		})
	}
}

func TestClientDoErrors(t *testing.T) {
	t.Parallel()

	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	sockPath := testSocketPath()
	svr, err := NewServer(sockPath, yesNoJSONServer{})
	if err != nil {
		t.Fatalf("NewServer(%q, yesNoJSONServer) = error %v", sockPath, err)
	}

	if err := svr.Start(); err != nil {
		t.Fatalf("srv.Start() = %v", err)
	}
	t.Cleanup(func() { svr.Close() }) //nolint:errcheck // best-effort cleanup in test

	cli, err := NewClient(ctx, sockPath, "alpaca")
	if err != nil {
		t.Fatalf("NewClient(ctx, %q, alpaca) = error %v", sockPath, err)
	}

	tests := []struct {
		method, url string
		want        APIErr
	}{
		{
			method: http.MethodPatch,
			url:    "http://yn/yes",
			want: APIErr{
				Msg:        "method not allowed",
				StatusCode: http.StatusMethodNotAllowed,
			},
		},
		{
			method: http.MethodGet,
			url:    "http://yn/maybe",
			want: APIErr{
				Msg:        "not found",
				StatusCode: http.StatusNotFound,
			},
		},
		{
			method: http.MethodGet,
			url:    "http://yn/secret",
			want: APIErr{
				Msg:        "invalid authorization token",
				StatusCode: http.StatusUnauthorized,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.url, func(t *testing.T) {
			t.Parallel()
			var dummy messageResponse
			err := cli.Do(ctx, test.method, test.url, nil, &dummy)
			if diff := cmp.Diff(err, test.want); diff != "" {
				t.Errorf("cli.Do(ctx, %q, %q, nil, &dummy) error diff (-got +want)\n%s", test.method, test.url, diff)
			}
		})
	}
}
