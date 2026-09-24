package dockerbootstrap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNetworkLifecycle(t *testing.T) {
	for _, failure := range []string{"", "network create", "create", "start"} {
		t.Run("failure="+failure, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.Environment = append(cfg.Environment, "BUILDKITE_JOB_ID=test-job", "BUILDKITE_AGENT_ID=test-agent", "BUILDKITE_AGENT_NAME=test-worker")
			f := &fakeClient{run: func(_ context.Context, args []string, _ map[string]string, _, _ io.Writer) (int, error) {
				action := args[0]
				if action == "network" {
					action += " " + args[1]
				}
				if action == failure {
					return 1, fmt.Errorf("injected failure")
				}
				return 0, nil
			}}
			code, err := (Runner{Client: f}).Run(t.Context(), cfg)
			if (failure == "" && (code != 0 || err != nil)) || (failure != "" && (code != SetupFailure || err == nil)) {
				t.Fatalf("code=%d err=%v", code, err)
			}
			var network string
			for i, args := range f.calls {
				switch args[0] {
				case "network":
					if args[1] == "create" {
						network = args[len(args)-1]
						if !strings.HasPrefix(network, "buildkite_network_") || !slices.Contains(args, "bridge") {
							t.Fatalf("unexpected network: %v", args)
						}
						for _, label := range []string{"com.buildkite.bootstrap=docker", "com.buildkite.agent=true", "com.buildkite.job-id=test-job", "com.buildkite.agent-id=test-agent", "com.buildkite.agent-name=test-worker"} {
							if !slices.Contains(args, label) {
								t.Errorf("missing network label %s", label)
							}
						}
					}
					if args[1] == "rm" && (i != len(f.calls)-1 || args[2] != network) {
						t.Fatalf("network must be removed last: %v", f.calls)
					}
				case "create":
					if network == "" || args[slices.Index(args, "--network")+1] != network {
						t.Fatalf("container not attached to its network: %v", args)
					}
					if args[slices.Index(args, "--name")+1] != strings.Replace(network, "buildkite_network_", "buildkite_job_", 1) {
						t.Fatalf("resource suffixes differ: %v", args)
					}
				}
			}
			last := f.calls[len(f.calls)-1]
			if !slices.Equal(last, []string{"network", "rm", network}) {
				t.Fatalf("network cleanup missing: %v", f.calls)
			}
			if f.called("rm") != (failure != "network create") {
				t.Fatal("incorrect partial-container cleanup")
			}
		})
	}
}

func TestNetworkCreateCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := &fakeClient{run: func(ctx context.Context, args []string, _ map[string]string, _, _ io.Writer) (int, error) {
		if args[0] == "network" && args[1] == "create" {
			cancel()
			return 0, ctx.Err()
		}
		if args[0] == "network" && args[1] == "rm" && ctx.Err() != nil {
			t.Error("network cleanup inherited cancellation")
		}
		return 0, nil
	}}
	code, err := (Runner{Client: f}).Run(ctx, testConfig(t))
	if code != SetupFailure || err == nil || f.called("create") {
		t.Fatalf("code=%d err=%v calls=%v", code, err, f.calls)
	}
	if last := f.calls[len(f.calls)-1]; !slices.Equal(last[:2], []string{"network", "rm"}) {
		t.Fatal("missing network cleanup")
	}
}

func TestNetworkCleanupFailure(t *testing.T) {
	for _, jobCode := range []int{0, 42} {
		for _, absent := range []bool{false, true} {
			t.Run(fmt.Sprintf("exit=%d/absent=%t", jobCode, absent), func(t *testing.T) {
				f := &fakeClient{run: func(_ context.Context, args []string, _ map[string]string, out, _ io.Writer) (int, error) {
					if args[0] == "start" {
						return jobCode, nil
					}
					if args[0] == "network" {
						switch args[1] {
						case "rm":
							return 1, nil
						case "ls":
							if !absent {
								_, _ = io.WriteString(out, "remaining-network")
							}
						}
					}
					return 0, nil
				}}
				var stderr bytes.Buffer
				code, err := (Runner{Client: f, Stderr: &stderr}).Run(t.Context(), testConfig(t))
				want := jobCode
				if !absent && jobCode == 0 {
					want = SetupFailure
				}
				if code != want || (err != nil) != (!absent && jobCode == 0) {
					t.Fatalf("code=%d err=%v", code, err)
				}
				if strings.Contains(stderr.String(), "network may remain") == absent {
					t.Fatalf("unexpected diagnostic: %s", stderr.String())
				}
			})
		}
	}
}

func TestContainerCleanupReservesNetworkBudget(t *testing.T) {
	f := &fakeClient{run: func(ctx context.Context, args []string, _ map[string]string, _, _ io.Writer) (int, error) {
		if args[0] == "rm" || args[0] == "ps" {
			<-ctx.Done()
			return 0, ctx.Err()
		}
		if args[0] == "network" && args[1] == "rm" {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) <= 0 {
				t.Error("container cleanup exhausted network budget")
			}
		}
		return 0, nil
	}}
	code, err := (Runner{Client: f}).Run(t.Context(), testConfig(t))
	if code != SetupFailure || err == nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
}
