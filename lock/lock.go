// Package lock provides a client for the Agent API locking service. This is
// intended to be used both internally by the agent itself, as well as by
// authors of binary hooks or other programs.
package lock

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/internal/agentapi"
)

// For local sockets, we can afford to be fairly chatty. 100ms is an arbitrary
// choice which is simultaneously "a long time" (for a computer) and
// "the blink of an eye" (for a human).
const localSocketSleepDuration = 100 * time.Millisecond

// Client implements a client library for the Agent API locking service.
type Client struct {
	client *agentapi.Client
}

// NewClient creates a new machine-scope lock service client.
func NewClient(ctx context.Context, socketsDir string) (*Client, error) {
	cli, err := agentapi.NewClient(ctx, agentapi.LeaderPath(socketsDir))
	if err != nil {
		return nil, err
	}
	return &Client{client: cli}, nil
}

// Get retrieves the current state of a lock.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	return c.client.LockGet(ctx, key)
}

// Locker returns a sync.Mutex-like object that uses the client to perform
// locking. Any errors encountered by the client while locking or unlocking
// (for example, the agent running the API stops running) will cause a panic
// (because sync.Locker has no other way to report an error). For greater
// flexibility, use Client's Lock and Unlock methods directly.
func (c *Client) Locker(key string) sync.Locker {
	return &locker{
		client: c,
		key:    key,
	}
}

// Lock blocks until the lock for the given key is acquired. It returns a
// token or an error. The token must be passed to Unlock in order to unlock the
// lock later on.
func (c *Client) Lock(ctx context.Context, key string) (string, error) {
	// The token generation only has to avoid making the same token twice to
	// prevent separate processes unlocking each other.
	// Using crypto/rand to generate 16 bytes is possibly overkill - it's not a
	// goal to be cryptographically secure - but ensures the result.
	otp := make([]byte, 16)
	if _, err := rand.Read(otp); err != nil {
		return "", err
	}
	token := fmt.Sprintf("acquired(pid=%d,otp=%x)", os.Getpid(), otp)

	for {
		_, done, err := c.client.LockCompareAndSwap(ctx, key, "", token)
		if err != nil {
			return "", fmt.Errorf("cas: %w", err)
		}

		if done {
			return token, nil
		}

		// Not done.
		if err := sleep(ctx, localSocketSleepDuration); err != nil {
			return "", err
		}
	}
}

// Unlock unlocks the lock for the given key. To prevent different processes
// accidentally unlocking the same lock, token must match the current lock value.
func (c *Client) Unlock(ctx context.Context, key, token string) error {
	val, done, err := c.client.LockCompareAndSwap(ctx, key, token, "")
	if err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	if !done {
		if val == "" {
			return errors.New("already unlocked")
		}
		return fmt.Errorf("invalid lock state %q", val)
	}
	return nil
}

// DoOnce is similar to sync.Once. In the absence of an error, it does one of
// two things:
//   - Calls f, and returns when done.
//   - Waits until another invocation with this key has completed, and
//     then returns.
//
// Like sync.Once, if f panics, DoOnce considers it to have returned.
func (c *Client) DoOnce(ctx context.Context, key string, f func()) (err error) {
	do, err := c.DoOnceStart(ctx, key)
	if err != nil {
		return err
	}
	if !do {
		// Already done
		return nil
	}

	// To do like sync.Once on panic, we must mark it done from a defer.
	defer func() {
		err = c.DoOnceEnd(ctx, key)
	}()

	// Lock acquired, do the work.
	f()
	return nil
}

// DoOnceStart begins a do-once section. It reports if the operation to perform
// exactly once should proceed.
func (c *Client) DoOnceStart(ctx context.Context, key string) (bool, error) {
	// The work could already be done, so start by getting the current state.
	state, err := c.client.LockGet(ctx, key)
	if err != nil {
		return false, err
	}
	for {
		switch state {
		case "":
			// Try to acquire the lock by transitioning to state "doing"
			st, done, err := c.client.LockCompareAndSwap(ctx, key, "", "doing")
			if err != nil {
				return false, fmt.Errorf("cas: %w", err)
			}
			if !done {
				// Lock not acquired (perhaps something else acquired it).
				state = st
				continue
			}

			return true, nil

		case "doing":
			// Work in progress - wait until state "done".
			if err := sleep(ctx, localSocketSleepDuration); err != nil {
				return false, err
			}
			st, err := c.client.LockGet(ctx, key)
			if err != nil {
				return false, err
			}
			state = st

		case "done":
			// Work already completed!
			return false, nil

		default:
			// Invalid state.
			return false, fmt.Errorf("invalid lock state %q", state)
		}
	}
}

// DoOnceEnd marks a do-once section as completed.
func (c *Client) DoOnceEnd(ctx context.Context, key string) error {
	st, done, err := c.client.LockCompareAndSwap(ctx, key, "doing", "done")
	if err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	if !done {
		return fmt.Errorf("invalid lock state %q", st)
	}
	return nil
}

type locker struct {
	client     *Client
	mu         sync.Mutex
	key, token string
}

func (l *locker) Lock() {
	l.mu.Lock()
	// sync.Mutex waits forever for a lock, so we do the same thing (i.e. the
	// background context).
	token, err := l.client.Lock(context.Background(), l.key)
	if err != nil {
		panic(err)
	}
	l.token = token
}

func (l *locker) Unlock() {
	if err := l.client.Unlock(context.Background(), l.key, l.token); err != nil {
		panic(err)
	}
	l.token = ""
	l.mu.Unlock()
}

// sleep sleeps in a context-aware way. The only non-nil errors returned are
// from ctx.Err.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
