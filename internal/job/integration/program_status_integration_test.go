package integration

import (
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/agent/plugin"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/bintest/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/wolfeidau/osc7501"
)

func TestProgramStatusReportsPhasesAndHooks(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the plugin hook is a bash script")
	}

	ctx, _ := experiments.Enable(mainCtx, experiments.ProgramStatus)
	tester, err := NewExecutorTester(ctx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	p := createTestPlugin(t, map[string][]string{
		"environment": {"#!/usr/bin/env bash", "true"},
	})
	pluginsJSON, err := p.ToJSON()
	if err != nil {
		t.Fatalf("testPlugin.ToJSON() error = %v", err)
	}
	plugins, err := plugin.CreateFromJSON(pluginsJSON)
	if err != nil {
		t.Fatalf("plugin.CreateFromJSON() error = %v", err)
	}
	pluginName := plugins[0].Name()

	tester.ExpectGlobalHook("pre-command").Once().AndExitWith(0)
	tester.ExpectGlobalHook("command").Once().AndExitWith(2)

	if err := tester.Run(t, "BUILDKITE_PLUGINS="+pluginsJSON, "BUILDKITE_PROGRAM_STATUS=true"); err == nil {
		t.Fatalf("tester.Run() succeeded, want the command to fail. Full output: %s", tester.Output)
	}

	var got []string
	scanner := osc7501.Scanner{W: io.Discard, OnReport: func(r osc7501.Report) {
		if r.App == "buildkite-agent" {
			got = append(got, strings.TrimSpace(r.State+" "+r.ID+" "+r.Msg))
		}
	}}
	_, _ = scanner.Write([]byte(tester.Output))

	pluginHook := "hook/plugin/" + pluginName + "/environment"
	want := []string{
		"working " + pluginHook + " Running plugin " + pluginName + " environment hook",
		"done " + pluginHook,
		"working checkout Checking out the repository",
		"done checkout",
		"working hook/agent/pre-command Running agent pre-command hook",
		"done hook/agent/pre-command",
		"working command Running the command",
		"working hook/agent/command Running agent command hook",
		"error hook/agent/command The agent command hook exited with status 2",
		"error command The command exited with status 2",
		// When the job ends, records that finished successfully are cleared.
		"clear " + pluginHook,
		"clear checkout",
		"clear hook/agent/pre-command",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("program status reports diff (-want +got):\n%s\nFull output: %s", diff, tester.Output)
	}

	// The agent's own reports already have a header in the log.
	if strings.Contains(tester.Output, "# Program status:") {
		t.Errorf("output describes the agent's own reports. Full output: %s", tester.Output)
	}
}

func TestProgramStatusCancelledCommandIsIdle(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("cancellation uses SIGINT")
	}

	ctx, _ := experiments.Enable(mainCtx, experiments.ProgramStatus)
	tester, err := NewExecutorTester(ctx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	tester.MustMock(t, "blocking-command").Expect().Once().AndCallFunc(func(c *bintest.Call) {
		close(started)
		<-release
		c.Exit(1)
	})

	runDone := make(chan error, 1)
	go func() {
		runDone <- tester.Run(t, "BUILDKITE_COMMAND=blocking-command", "BUILDKITE_PROGRAM_STATUS=true")
	}()
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for command to start")
	}
	if err := tester.Cancel(); err != nil {
		t.Fatalf("tester.Cancel() = %v", err)
	}
	select {
	case <-runDone:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for cancelled job to finish")
	}

	states := make(map[string]string) // id -> last state
	scanner := osc7501.Scanner{W: io.Discard, OnReport: func(r osc7501.Report) {
		if r.App == "buildkite-agent" {
			states[r.ID] = r.State
		}
	}}
	_, _ = scanner.Write([]byte(tester.Output))

	if got := states["command"]; got != osc7501.StateIdle {
		t.Errorf("command record ended %q, want %q. Full output: %s", got, osc7501.StateIdle, tester.Output)
	}
}
