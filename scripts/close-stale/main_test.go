package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	for _, tc := range []struct {
		months int
		year   int
		month  time.Month
		dryRun bool
		period string
	}{
		{12, 2026, time.April, true, "12 months"},
		{12, 2026, time.April, false, "12 months"},
		{18, 2026, time.October, true, "18 months"},
		{18, 2026, time.October, false, "18 months"},
		{1, 2025, time.May, false, "1 month"},
	} {
		t.Run(fmt.Sprintf("months=%d/dry-run=%t", tc.months, tc.dryRun), func(t *testing.T) {
			var logs bytes.Buffer
			previousOutput := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previousOutput) })
			dryRun := tc.dryRun
			closed := make(map[string]map[string]string)
			comments := make(map[string]string)
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing authorization")
				}

				if r.Method == http.MethodPost {
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}

					comments[r.URL.Path] = body["body"]
					w.WriteHeader(http.StatusCreated)
					_, _ = io.WriteString(w, `{}`)
					return
				}

				if r.Method == http.MethodPatch {
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}

					closed[r.URL.Path] = body
					_, _ = io.WriteString(w, `{}`)
					return
				}

				switch r.URL.Path {
				case "/issues":
					// Items 5 and 6 are exactly at and just after the cutoff.
					_, _ = io.WriteString(w, `[
						{"number":1,"state":"open","updated_at":"2025-04-19T03:00:00Z"},
						{"number":2,"state":"open","updated_at":"2025-04-19T03:00:00Z","pull_request":{}},
						{"number":3,"state":"open","updated_at":"2025-04-19T03:00:00Z","pull_request":{}},
						{"number":4,"state":"open","updated_at":"2025-04-19T03:00:00Z"},
						{"number":5,"state":"open","updated_at":"2025-04-20T03:00:00Z"},
						{"number":6,"state":"open","updated_at":"2025-04-20T03:00:01Z"}
					]`)
				case "/issues/1", "/pulls/2":
					_, _ = io.WriteString(w, `{"state":"open","updated_at":"2025-04-19T03:00:00Z","html_url":"https://github.com/buildkite/agent"}`)
				case "/pulls/3":
					// A PR updated since the listing must not be closed.
					_, _ = io.WriteString(w, `{"state":"open","updated_at":"2026-10-20T02:00:00Z"}`)
				case "/issues/4":
					_, _ = io.WriteString(w, `{"state":"closed","updated_at":"2025-04-19T03:00:00Z"}`)
				default:
					t.Errorf("unexpected request: %s", r.URL)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := &githubClient{baseURL: server.URL, token: "test-token", http: server.Client()}
			now := time.Date(tc.year, tc.month, 20, 3, 0, 0, 0, time.UTC)
			if err := run(t.Context(), client, now, tc.months, dryRun); err != nil {
				t.Fatal(err)
			}

			mu.Lock()
			defer mu.Unlock()
			want := make(map[string]map[string]string)
			if !dryRun {
				want = map[string]map[string]string{
					"/issues/1": {"state": "closed", "state_reason": "not_planned"},
					"/pulls/2":  {"state": "closed"},
				}
			}

			if !reflect.DeepEqual(closed, want) {
				t.Errorf("closed = %v, want %v", closed, want)
			}

			wantComments := make(map[string]string)
			if !dryRun {
				wantComments = map[string]string{
					"/issues/1/comments": "This issue has been closed because it's been inactive for more than " + tc.period + ". If you'd like to revive the work and are ready to take the lead on moving it forward, feel free to reopen it.",
					"/issues/2/comments": "This PR has been closed because it's been inactive for more than " + tc.period + ". If you'd like to revive the work and are ready to take the lead on moving it forward, feel free to reopen it.",
				}
			}

			if !reflect.DeepEqual(comments, wantComments) {
				t.Errorf("comments = %v, want %v", comments, wantComments)
			}

			wantSummary := "Summary (completed): 2 closed; 2 comments posted; 2 skipped\n"
			if dryRun {
				wantSummary = "Summary (completed, dry run): 2 would close; 2 skipped\n"
			}

			if !strings.HasSuffix(logs.String(), wantSummary) {
				t.Errorf("logs = %q, want final summary %q", logs.String(), wantSummary)
			}
		})
	}
}

func TestRunPagination(t *testing.T) {
	closed := make(map[int]bool)
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/issues" {
			// Model the real API: closing an issue removes it from later pages.
			var open []item
			for n := 1; n <= 101; n++ {
				if !closed[n] {
					open = append(open, item{Number: n, State: "open", UpdatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)})
				}
			}

			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			start := min((page-1)*100, len(open))
			if err := json.NewEncoder(w).Encode(open[start:min(start+100, len(open))]); err != nil {
				t.Error(err)
			}

			return
		}

		n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/issues/"))
		if r.Method == http.MethodPatch {
			closed[n] = true
		}

		_, _ = io.WriteString(w, `{"state":"open","updated_at":"2020-01-01T00:00:00Z"}`)
	}))
	defer server.Close()
	client := &githubClient{baseURL: server.URL, http: server.Client()}
	if err := run(t.Context(), client, time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC), 12, false); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(closed) != 101 {
		t.Errorf("closed %d issues, want 101", len(closed))
	}
}

func TestRunCalendarMonths(t *testing.T) {
	for _, year := range []int{2025, 2026} {
		t.Run(strconv.Itoa(year), func(t *testing.T) {
			// Both leap and non-leap Februaries must clamp rather than roll
			// into March. March 1 is less than 18 months before August 31.
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/issues" {
					t.Errorf("should leave March activity alone: %s", r.URL)
				}

				_, _ = fmt.Fprintf(w, `[{"number":1,"state":"open","updated_at":"%d-03-01T00:00:00Z"}]`, year-1)
			}))
			defer server.Close()
			client := &githubClient{baseURL: server.URL, http: server.Client()}
			if err := run(t.Context(), client, time.Date(year, 8, 31, 3, 0, 0, 0, time.UTC), 18, false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRunAPIFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer server.Close()
	client := &githubClient{baseURL: server.URL, http: server.Client()}
	if err := run(t.Context(), client, time.Now().UTC(), 12, false); err == nil {
		t.Fatal("expected API failure to fail the build")
	}
}

func TestRunInvalidMonths(t *testing.T) {
	for _, months := range []int{0, -1} {
		if err := run(t.Context(), &githubClient{}, time.Now().UTC(), months, false); err == nil {
			t.Errorf("expected months=%d to be rejected before making API requests", months)
		}
	}
}

func TestRunSummaryAfterCommentFailure(t *testing.T) {
	var logs bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousOutput) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			http.Error(w, "forbidden", http.StatusForbidden)
		case r.URL.Path == "/issues":
			_, _ = io.WriteString(w, `[{"number":1,"state":"open","updated_at":"2020-01-01T00:00:00Z"}]`)
		default:
			_, _ = io.WriteString(w, `{"state":"open","updated_at":"2020-01-01T00:00:00Z"}`)
		}
	}))
	defer server.Close()
	client := &githubClient{baseURL: server.URL, http: server.Client()}
	if err := run(t.Context(), client, time.Now().UTC(), 12, false); err == nil {
		t.Fatal("expected comment failure to fail the build")
	}

	want := "Summary (failed): 1 closed; 0 comments posted; 0 skipped\n"
	if !strings.HasSuffix(logs.String(), want) {
		t.Errorf("logs = %q, want final summary %q", logs.String(), want)
	}
}

func TestRunConcurrency(t *testing.T) {
	var active, peak, closed, comments atomic.Int64
	started := make(chan struct{}, 31)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/issues":
			items := make([]item, 31)
			for n := range items {
				items[n] = item{Number: n + 1, State: "open", UpdatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
			}

			if err := json.NewEncoder(w).Encode(items); err != nil {
				t.Error(err)
			}
		case r.Method == http.MethodGet:
			n := active.Add(1)
			for old := peak.Load(); n > old; old = peak.Load() {
				if peak.CompareAndSwap(old, n) {
					break
				}
			}

			started <- struct{}{}
			<-release
			active.Add(-1)
			_, _ = io.WriteString(w, `{"state":"open","updated_at":"2020-01-01T00:00:00Z"}`)
		case r.Method == http.MethodPatch:
			closed.Add(1)
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost:
			comments.Add(1)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected method: %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)
	client := &githubClient{baseURL: server.URL, http: server.Client()}
	done := make(chan error, 1)
	go func() { done <- run(t.Context(), client, time.Now().UTC(), 12, false) }()
	for range 15 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("didn't reach 15 concurrent activity checks")
		}
	}

	select {
	case <-started:
		t.Error("started a 16th activity check while the first 15 were still blocked")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if peak.Load() != 15 || closed.Load() != 31 || comments.Load() != 31 {
		t.Errorf("peak=%d closed=%d comments=%d; want 15, 31, 31", peak.Load(), closed.Load(), comments.Load())
	}
}

func TestRunMutationPacing(t *testing.T) {
	var mu sync.Mutex
	var lastFinished time.Time
	var mutations int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Path == "/issues" {
				_, _ = io.WriteString(w, `[
					{"number":1,"state":"open","updated_at":"2020-01-01T00:00:00Z"},
					{"number":2,"state":"open","updated_at":"2020-01-01T00:00:00Z"}
				]`)
			} else {
				_, _ = io.WriteString(w, `{"state":"open","updated_at":"2020-01-01T00:00:00Z"}`)
			}

			return
		}

		if !mu.TryLock() {
			t.Error("mutations overlapped")
			mu.Lock()
		}

		defer mu.Unlock()

		if !lastFinished.IsZero() && time.Since(lastFinished) < time.Second {
			t.Errorf("mutation arrived %s after the previous one finished; want at least 1s", time.Since(lastFinished))
		}

		mutations++
		if mutations == 1 {
			// A slow response must not allow another mutation to start in parallel.
			time.Sleep(1100 * time.Millisecond)
		}

		_, _ = io.WriteString(w, `{}`)
		lastFinished = time.Now()
	}))
	defer server.Close()
	client := &githubClient{baseURL: server.URL, http: server.Client(), mutationInterval: time.Second}
	if err := run(t.Context(), client, time.Now().UTC(), 12, false); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	if mutations != 4 {
		t.Errorf("got %d mutations, want 2 closures and 2 comments", mutations)
	}
}
