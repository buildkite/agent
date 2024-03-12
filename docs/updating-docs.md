<!-- This doc is copied from the Buildkite engineering handbook, and is copied here so that it can be consumed by the public -->

# Updating Agent Docs

The public documentation for the agent is hosted at https://buildkite.com/docs/agent/v3. The source code is in https://github.com/buildkite/docs. This document is our guide on how to update the public docs as we develop the agent.

## New sub command

Creating a new sub command for the agent requires touching a few different files. In this example, our new sub command will be called `foo` and will be invoked as

```
buildkite-agent foo
```

The page for the sub command will be served at `/docs/agent/v3/cli-foo`

### Create ERB template

This will be at `pages/agent/v3/cli_foo.md.erb`. The template only contains some sections of the sub command, the rest is generated as a markdown file

### Add sub command to generation script

The script is:

```
scripts/update-agent-help.sh
```

There is a bash array called `commands` within it. You will need to add the sub command (i.e. foo) to it.

### Generate markdown

You will need to create an empty file in the right location and then execute the script.

```
touch pages/agent/v3/help/_foo.md
scripts/update-agent-help.sh
```

### Add to Usage section

In the file `pages/agent/v3.md.erb`, there is a H2 called "Usage" (i.e. ## Usage)
Under the text "Available commands are:", there are some anchors. The section is intended to represent the output of `buildkite-agent --help`. You will need to add an links to the sub command here:

```
<a href="/docs/agent/v3/cli-foo">foo</a>    Description of foo that is in the help text
```

As well as using the exact same description of `foo` that is in the help text, you MUST ensure that the number of spaces between the closing anchor tag an the start of the description allows the correct alignment of the start of the description for foo with those of the other commands. See https://buildkite.com/docs/agent/v3#usage for what this currently looks like. You should view docs served locally or on Render to iterate on this.

### Add to nav bar

We need to add the yaml object

```
              - name: 'foo'
                path: 'agent/v3/cli-foo'
```

To the children of an object with the name `Command-line reference` in the file `data/nav.yml`.

### Example

Here is an example of a PR that added a sub command and little else: https://github.com/buildkite/docs/pull/1742
