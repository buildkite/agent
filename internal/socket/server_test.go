package socket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

var testSocketCounter uint32

func testSocketPath() string {
	id := atomic.AddUint32(&testSocketCounter, 1)
	return filepath.Join(os.TempDir(), fmt.Sprintf("test-%d-%d", os.Getpid(), id))
}

type yesNoServer struct{}

func (yesNoServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch r.URL.Path {
	case "/yes":
		w.Write([]byte("Yes!\n")) //nolint:errcheck // test handler
	case "/no":
		w.Write([]byte("No.\n")) //nolint:errcheck // test handler
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func TestServerStartStop(t *testing.T) {
	t.Parallel()

	sockPath := testSocketPath()
	svr, err := NewServer(sockPath, yesNoServer{})
	if err != nil {
		t.Fatalf("NewServer(%q, yesNoServer) = error %v", sockPath, err)
	}

	if err := svr.Start(); err != nil {
		t.Fatalf("srv.Start() = %v", err)
	}

	// Check the socket path exists and is a socket.
	// Note that os.ModeSocket might not be set on Windows.
	// (https://github.com/golang/go/issues/33357)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(sockPath)
		if err != nil {
			t.Fatalf("os.Stat(%q) = %v", sockPath, err)
		}

		if fi.Mode()&os.ModeSocket == 0 {
			t.Fatalf("%q is not a socket", sockPath)
		}
	}

	// Try to connect to the socket.
	test, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("socket test connection: %v", err)
	}

	test.Close() //nolint:errcheck // test connection verified; close is best-effort

	if err := svr.Close(); err != nil {
		t.Fatalf("svr.Close() = %v", err)
	}

	time.Sleep(100 * time.Millisecond) // Wait for the socket file to be unlinked
	_, err = os.Stat(sockPath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.Stat(%s) = _, os.ErrNotExist, got %v", sockPath, err)
	}
}

func TestServerHandler(t *testing.T) {
	t.Parallel()

	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	sockPath := testSocketPath()
	svr, err := NewServer(sockPath, yesNoServer{})
	if err != nil {
		t.Fatalf("NewServer(%q, yesNoServer) = error %v", sockPath, err)
	}

	if err := svr.Start(); err != nil {
		t.Fatalf("svr.Start() = %v", err)
	}
	t.Cleanup(func() { svr.Close() }) //nolint:errcheck // best-effort cleanup in test

	cli := &http.Client{
		Transport: &http.Transport{
			Dial: func(string, string) (net.Conn, error) {
				return net.Dial("unix", sockPath)
			},
		},
	}

	tests := []struct {
		method, url, want string
		wantStatus        int
	}{
		{
			method:     http.MethodGet,
			url:        "http://yn/yes",
			wantStatus: http.StatusOK,
			want:       "Yes!\n",
		},
		{
			method:     http.MethodGet,
			url:        "http://yn/no",
			wantStatus: http.StatusOK,
			want:       "No.\n",
		},
		{
			method:     http.MethodGet,
			url:        "http://yn/maybe",
			wantStatus: http.StatusNotFound,
			want:       "not found\n",
		},
		{
			method:     http.MethodPatch,
			url:        "http://yn/yes",
			wantStatus: http.StatusMethodNotAllowed,
			want:       "method not allowed\n",
		},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.url, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(ctx, test.method, test.url, nil)
			if err != nil {
				t.Fatalf("http.NewRequest(%q, %q, nil) = error %v", test.method, test.url, err)
			}
			resp, err := cli.Do(req)
			if err != nil {
				t.Fatalf("cli.Do(req) = error %v", err)
			}
			defer resp.Body.Close() //nolint:errcheck // response body close errors are inconsequential in tests
			if got, want := resp.StatusCode, test.wantStatus; got != want {
				t.Errorf("resp.Status = %v, want %v", got, want)
			}
			got, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("io.ReadAll(resp.Body) = error %v", err)
			}
			if got, want := string(got), test.want; got != want {
				t.Errorf("resp.Body = %q, want %q", got, want)
			}
		})
	}
}
