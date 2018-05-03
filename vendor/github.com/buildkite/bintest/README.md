Bintest
=======

A set of tools for generating fake binaries that can be used for testing. A binary is compiled and then can be orchestrated from your test suite and later checked for assertions.

Mocks can communicate and respond in real-time with the tests that are calling them, which allows for testing complicated dependencies. See https://github.com/buildkite/agent/tree/master/bootstrap/integration for how we use it to test buildkite-agent's bootstrap process.

Mocks
-----

Mocks are your typical mock object, but as an executable that your code can shell out to and then later test assertions on.

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

agent.AssertExpectations(t)
```

Proxies
-------

Proxies are what power Mocks.

```go
// Compile a proxy for the git command that echos some debug
proxy, err := bintest.CompileProxy("git")
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

Inspired by [bats-mock](https://github.com/jasonkarns/bats-mock) and [go-binmock](https://github.com/pivotal-cf/go-binmock).
