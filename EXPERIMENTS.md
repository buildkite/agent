# Experiments

We frequently introduce new experimental features to the agent. You can use the `--experiment` flag to opt-in to them and test them out:

```bash
buildkite-agent start --experiment experiment1 --experiment expertiment2
```

Or you can set them in your [agent configuration file](https://buildkite.com/docs/agent/v3/configuration):

```
experiment="experiment1,experiment2"
```

If an experiment doesn't exist, no error will be raised.

**Please note that there is every chance we will remove or change these experiments, so using them should be at your own risk and without the expectation that they will work in future!**

## Available Experiments

### `agent-socket`

The agent currently exposes a per-session token to jobs called `BUILDKITE_AGENT_ACCESS_TOKEN`. This token can be used for pipeline uploads, meta-data get/set and artifact access within the job. Leaking it in logging can be dangerous, as anyone with that token can access whatever your agent could.

The agent socket experiment creates a local proxy for the Agent API with a single-use api token. This means it's impossible to leak this information outside of a job. On Windows, this uses a local HTTP bind, and a unix domain socket on other systems.

**Status**: broadly useful, we'd like this to be the standard behaviour. 👌

### `msgpack`

Agent registration normally uses a REST API with a JSON framing. This experiment uses [msgpack](https://msgpack.org/) with the aim of lower latency and reduced network traffic footprint.

**Status**: Internal experiment and depends on experimental backend support. Probably not broadly useful yet! 🙅🏼
