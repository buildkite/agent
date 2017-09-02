package bootstrap

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"testing"

	"github.com/buildkite/agent/bootstrap/shell"
	"github.com/stretchr/testify/assert"
)

// go run *.go bootstrap --job "llamas" --repository "git@github.com:buildkite/agent.git" --debug --commit "HEAD" --branch "master" --agent "my-agent" --organization "buildkite" --pipeline "agent" --pipeline-provider git --build-path /usr/local/var/buildkite-agent/builds --command "pwd"

func TestRunningBootstrapIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	dir, err := ioutil.TempDir("", "bootstrap-test")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}

	if !testing.Verbose() {
		sh.Logger = shell.DiscardLogger
		sh.Output = ioutil.Discard
	}

	b := Bootstrap{
		Config: Config{
			JobID:            "test-job",
			Repository:       "https://github.com/buildkite/bash-example.git",
			Debug:            true,
			Commit:           "HEAD",
			Branch:           "master",
			AgentName:        "test-agent",
			OrganizationSlug: "buildkite",
			PipelineSlug:     "bash-example",
			PipelineProvider: "git",
			BuildPath:        dir,
			HooksPath:        "/dev/null",
			Command:          "echo hello world",
			GitCleanFlags:    "-fxdq",
			CommandEval:      true,
		},
		shell:    sh,
		exitFunc: os.Exit,
	}

	b.Start()
}

func TestDirForAgentName(t *testing.T) {
	var testCases = []struct {
		agentName string
		expected  string
	}{
		{"My Agent", "My-Agent"},
		{":docker: My Agent", "-docker--My-Agent"},
		{"My \"Agent\"", "My--Agent-"},
	}

	for _, test := range testCases {
		t.Run(fmt.Sprintf("%s => %s", test.agentName, test.expected), func(t *testing.T) {
			assert.Equal(t, test.expected, dirForAgentName(test.agentName))
		})
	}
}
