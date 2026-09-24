package dockerbootstrap

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPullPolicies(t *testing.T) {
	for _, policy := range []string{"", "missing", "always", "never"} {
		for _, cached := range []bool{false, true} {
			for _, pullFails := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/cached=%t/pullFails=%t", policy, cached, pullFails), func(t *testing.T) {
					cfg := testConfig(t)
					cfg.PullPolicy = policy
					f := &fakeClient{run: func(_ context.Context, args []string, _ map[string]string, _, _ io.Writer) (int, error) {
						if args[0] == "image" && !slices.Contains(args, "--format") && !cached {
							return 1, nil
						}
						if args[0] == "pull" && pullFails {
							return 1, nil
						}
						return 0, nil
					}}
					code, err := (Runner{Client: f}).Run(t.Context(), cfg)
					wantPull := policy == "always" || (!cached && policy != "never")
					wantFail := (!cached && policy == "never") || (wantPull && pullFails)
					if f.called("pull") != wantPull || (err != nil) != wantFail {
						t.Fatalf("pull=%t code=%d err=%v", f.called("pull"), code, err)
					}
					if wantFail && f.called("create") {
						t.Fatal("created container after failed image resolution")
					}
				})
			}
		}
	}
	cfg := testConfig(t)
	cfg.PullPolicy = "sometimes"
	if _, err := prepare(cfg); err == nil {
		t.Fatal("accepted invalid policy")
	}
}

func TestContainerUser(t *testing.T) {
	for _, value := range []string{"", "0:0", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())} {
		t.Run(value, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.User = value
			job, err := prepare(cfg)
			if err != nil {
				t.Fatal(err)
			}
			uid, gid, err := containerUser(value)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(job.args, fmt.Sprintf("%d:%d", uid, gid)) {
				t.Fatal("missing container user")
			}
			if job.env["HOME"] != containerRoot+"/home" {
				t.Fatal("missing private home")
			}
			if !strings.Contains(strings.Join(job.args, " "), fmt.Sprintf("mode=0700,uid=%d,gid=%d", uid, gid)) {
				t.Fatal("incorrect home ownership")
			}
			passwd, err := os.ReadFile(filepath.Join(job.userDirectory, "passwd"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(passwd), fmt.Sprintf(":%d:%d:", uid, gid)) {
				t.Fatal("missing user identity")
			}
		})
	}
	for _, value := range []string{"root", "1000", "-1:0", "1:-1", "1:2:3", "4294967295:0"} {
		if _, _, err := containerUser(value); err == nil {
			t.Errorf("accepted %s", value)
		}
	}
}
