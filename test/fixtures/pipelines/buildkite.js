plugin = require("buildkite/plugin");
require("buildkite/hello");

const dockerCompose = plugin("docker-compose", "v3.0.0", {
  config: ".buildkite/docker-compose.yml",
  run: "agent",
});

pipeline = {
  env: {
    DRY_RUN: !!process.env.DRY_RUN,
  },
  agents: {
    queue: "agent-runners-linux-amd64",
  },
  steps: [
    {
      name: ":go: go fmt",
      key: "test-go-fmt",
      command: ".buildkite/steps/test-go-fmt.sh",
      plugins: [dockerCompose],
    },
  ],
};

module.exports = pipeline;
