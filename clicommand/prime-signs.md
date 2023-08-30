′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′
Hey, that looks like a backtick, (`) doesn't it? Or kinda like a quote mark ('), maybe?
If you're here, you might be wondering why we use a strange, non-ascii character for marking inline code blocks in
CLI helptext, ie:
```
--disconnect-after-job  Disconnect the agent after running exactly one job. When used in conjunction with the ′--spawn′ flag, each worker booted will run exactly one job [$BUILDKITE_AGENT_DISCONNECT_AFTER_JOB]
```
The quotes around --spawn aren't backticks or quotes, but a [prime symbol](https://en.wikipedia.org/wiki/Prime_(symbol)). The reasoning behind this is that urfave/cli, our CLI library, has [special handling](https://cli.urfave.org/v1/examples/flags/#placeholder-values) for backticks (`) in helptext, which in our case leads to some (but not all 🙃) of the backticks getting swallowed.

Our workaround for this is to use symbols that look kinda, but not exactly like backticks. Oy.
′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′′
