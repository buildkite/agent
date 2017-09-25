Bintest
=======

A set of tools for generating binaries for testing. Mock objects are the primary usage,
built on top of a general Proxy object that allows for binaries to be added to a system under test
that are controlled from the main test case.

Mocks
-----

```go
agent, err := bintest.Mock("buildkite-agent")
if err != nil {
  t.Fatal(err)
}

agent.
  Expect("meta-data", "exists", "buildkite:git:commit").
  AndExitWith(1)
agent.
  Expect("meta-data", "set", mock.MatchAny()).
  AndExitWith(0)
agent.
  Expect("meta-data", "set", "buildkite:git:branch", mock.MatchAny()).
  AndExitWith(0)

agent.AssertExpectations(t)
```

Proxies
-------

```go
// create a proxy for the git command that echos some debug
proxy, err := proxy.New("git")
if err != nil {
  log.Fatal(err)
}

// call the proxy like a normal binary
go fmt.Println(exec.Command("git", "test", "arguments").CombinedOutput())

// handle invocations of the proxy binary
for call := range proxy.Ch {
  fmt.Fprintln(call.Stdout, "Llama party! 🎉")
  call.Exit(0)
}

// Llama party! 🎉
```

Credit
------

Inspired by bats-mock and go-binmock.
