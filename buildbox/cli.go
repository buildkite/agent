package buildbox

import (
  "flag"
  "fmt"
  "os"
)

type Cli struct {
  AccessToken string
  URL string
  Debug bool
  ExitOnComplete bool
}

func usage() {
  output := `The agent performs builds and sends the results back to Buildbox.

Usage:

  buildbox-agent command [arguments]

The comamnds are:

  start # start the buildbox agent

Use "buildbox-agent help [command]" for more information about a command.
`

  fmt.Println(output)
}

func (cli *Cli) Run() {
  // Set the flags that we'll use
  accessToken := flag.String("access-token", "", "The access token used to identify the agent.")
  debug := flag.Bool("debug", false, "Runs the agent in debug mode.")
  url := flag.String("url", "", "Specify a different API endpoint.")
  version := flag.Bool("version", false, "Show the version of the agent")
  exitOnComplete := flag.Bool("exit-on-complete", false, "Runs all available jobs and then exits")

  // Parse the flags
  flag.Usage = usage
  flag.Parse()

  // Load the data into our cli struct
  cli.AccessToken = *accessToken
  cli.URL = *url
  cli.Debug = *debug
  cli.ExitOnComplete = *exitOnComplete

  // No command has been specified
  if len(os.Args) == 1 {
    usage()
    os.Exit(1)
  }

  command := os.Args[1]

  // Are they trying to show the version?
  if *version {
    fmt.Printf("buildbox-agent version %s\n", Version)
    os.Exit(0)
  }

  // Trying to show the help information?
  if command == "help" {
    // Command specifc help
    if len(os.Args) == 2 {

    }

    usage()
    os.Exit(0)
  }

  if command == "start" {
    fmt.Printf("start some codez")
  }

  // No command could be found
  fmt.Printf("buildbox-agent: unknown command \"%s\"\n", command)
  fmt.Println("Run 'buildbox-agent help' for usage.")
  os.Exit(1)
}
