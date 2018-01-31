package integration

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/clicommand"
	"github.com/buildkite/bintest"
	"github.com/urfave/cli"
)

func TestMain(m *testing.M) {
	// Act as a bintest proxy stub if not integration.test
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") != `integration.test` {
		os.Exit(bintest.NewClientFromEnv().Run())
	}

	// If we are passed "bootstrap", execute like the bootstrap cli
	if len(os.Args) > 1 && os.Args[1] == `bootstrap` {
		app := cli.NewApp()
		app.Name = "buildkite-agent"
		app.Version = agent.Version()
		app.Commands = []cli.Command{
			clicommand.BootstrapCommand,
		}

		if err := app.Run(os.Args); err != nil {
			fmt.Printf("%v\n", err)
			os.Exit(1)
		}

		os.Exit(0)
	}

	initialGoRoutines := runtime.NumGoroutine()
	code := m.Run()

	// make sure all our bintest proxies are stopped
	if err := bintest.StopServer(); err != nil {
		log.Fatal(err)
	}

	// check for leaking go routines
	if code == 0 && !testing.Short() {

		// give things time to shutdown
		time.Sleep(time.Millisecond * 100)

		if runtime.NumGoroutine() > initialGoRoutines {
			log.Printf("There are %d go routines left running", runtime.NumGoroutine()-initialGoRoutines)
			log.Fatal(pprof.Lookup("goroutine").WriteTo(os.Stdout, 1))
		}
	}

	os.Exit(code)
}
