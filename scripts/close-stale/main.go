// close-stale closes buildkite/agent issues and PRs after a configurable period of inactivity.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

func main() {
	dryRun := flag.Bool("dry-run", true, "list stale items without closing them")
	months := flag.Int("months", 12, "close items inactive for more than this many calendar months (must be positive)")
	flag.Parse()
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		log.Fatal("GH_TOKEN is required")
	}

	client := &githubClient{
		baseURL:          "https://api.github.com/repos/buildkite/agent",
		token:            token,
		http:             &http.Client{Timeout: time.Minute},
		mutationInterval: time.Second,
	}
	if err := run(context.Background(), client, time.Now().UTC(), *months, *dryRun); err != nil {
		log.Fatal(err)
	}
}

type githubClient struct {
	baseURL string
	token   string
	http    *http.Client

	mutationMu       sync.Mutex
	mutationInterval time.Duration
	nextMutation     time.Time
}

func (c *githubClient) request(ctx context.Context, method, path, body string, result any) error {
	if method == http.MethodPatch || method == http.MethodPost {
		// Share one queue across closures and comments, leaving reads concurrent.
		c.mutationMu.Lock()
		defer c.mutationMu.Unlock()

		if delay := time.Until(c.nextMutation); delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}

		defer func() { c.nextMutation = time.Now().Add(c.mutationInterval) }()
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, strings.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "buildkite-agent-stale-cleanup")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}

	defer resp.Body.Close() //nolint:errcheck // This is idiomatic for response bodies.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}

	if result == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decoding %s: %w", path, err)
	}

	return nil
}

type item struct {
	Number      int       `json:"number"`
	State       string    `json:"state"`
	UpdatedAt   time.Time `json:"updated_at"`
	URL         string    `json:"html_url"`
	PullRequest *struct{} `json:"pull_request,omitempty"`
}

func (i item) stale(cutoff time.Time) bool {
	return i.State == "open" && i.UpdatedAt.Before(cutoff)
}

func run(ctx context.Context, client *githubClient, now time.Time, months int, dryRun bool) (err error) {
	if months <= 0 {
		return fmt.Errorf("months must be positive, got %d", months)
	}

	var wouldClose, closed, commented, skipped atomic.Int64
	defer func() {
		status := "completed"
		if err != nil {
			status = "failed"
		}

		if dryRun {
			log.Printf("Summary (%s, dry run): %d would close; %d skipped", status, wouldClose.Load(), skipped.Load())
		} else {
			log.Printf("Summary (%s): %d closed; %d comments posted; %d skipped", status, closed.Load(), commented.Load(), skipped.Load())
		}
	}()

	// Clamp to the last day of the target month instead of letting AddDate
	// normalise e.g. 31 August minus 18 months into early March.
	month := time.Date(now.Year(), now.Month()-time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	lastDay := month.AddDate(0, 1, -1).Day()
	cutoff := time.Date(month.Year(), month.Month(), min(now.Day(), lastDay), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), time.UTC)
	log.Printf("Cutoff: %s; dry run: %t", cutoff.Format(time.RFC3339), dryRun)
	// Collect before closing anything so removing items from the open list
	// doesn't shift pagination and skip items. The issues endpoint includes PRs.
	var candidates []item
	for page := 1; ; page++ {
		var items []item
		path := fmt.Sprintf("/issues?state=open&sort=updated&direction=asc&per_page=100&page=%d", page)
		if err := client.request(ctx, http.MethodGet, path, "", &items); err != nil {
			return err
		}

		for _, i := range items {
			if i.stale(cutoff) {
				candidates = append(candidates, i)
			}
		}

		if len(items) < 100 || !items[len(items)-1].stale(cutoff) {
			break
		}
	}

	group, workCtx := errgroup.WithContext(ctx)
	group.SetLimit(15)
	for _, candidate := range candidates {
		if workCtx.Err() != nil {
			break
		}

		group.Go(func() error {
			if err := workCtx.Err(); err != nil {
				return err
			}

			// Use the parent context so an error on another item doesn't interrupt
			// the comment after this item's closure. Wait for all in-flight work.
			kind := "issues"
			name := "issue"
			body := `{"state":"closed","state_reason":"not_planned"}`
			if candidate.PullRequest != nil {
				kind = "pulls"
				name = "PR"
				body = `{"state":"closed"}`
			}

			path := fmt.Sprintf("/%s/%d", kind, candidate.Number)
			// Recheck activity and state, including the PR's own updated timestamp.
			var current item
			if err := client.request(ctx, http.MethodGet, path, "", &current); err != nil {
				return err
			}

			if !current.stale(cutoff) {
				skipped.Add(1)
				return nil
			}

			if dryRun {
				log.Printf("Would close %s", current.URL)
				wouldClose.Add(1)
				return nil
			}

			log.Printf("Closing %s", current.URL)
			if err := client.request(ctx, http.MethodPatch, path, body, nil); err != nil {
				return err
			}

			closed.Add(1)
			period := fmt.Sprintf("%d months", months)
			if months == 1 {
				period = "1 month"
			}

			message := fmt.Sprintf("This %s has been closed because it's been inactive for more than %s. If you'd like to revive the work and are ready to take the lead on moving it forward, feel free to reopen it.", name, period)
			comment, err := json.Marshal(map[string]string{"body": message})
			if err != nil {
				return err
			}

			// PR conversation comments use the issues endpoint too.
			path = fmt.Sprintf("/issues/%d/comments", candidate.Number)
			if err := client.request(ctx, http.MethodPost, path, string(comment), nil); err != nil {
				return err
			}

			commented.Add(1)
			return nil
		})
	}

	return group.Wait()
}
