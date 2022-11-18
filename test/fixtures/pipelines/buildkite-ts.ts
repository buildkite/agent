abstract class Modifier {
  abstract run(pipeline: Pipeline): Pipeline;
}

interface AppProps {
  pipeline: Pipeline,
}

class App {
  pipeline: Pipeline;

  constructor(props: AppProps) {
    this.pipeline = props.pipeline;
  }

  run(): any {
    return pipeline.compile()
  }

  apply(modifier: Modifier) {
    this.pipeline = modifier.run(pipeline)
  }
}

abstract class BaseStep {
  label: string;

  protected constructor(label: string) {
    this.label = label;
  }

  abstract compile(): any;
}

class CommandStep extends BaseStep {
  key: string;
  command: string;
  priority: number | null;

  constructor(label: string, key: string, command: string, priority: number = null) {
    super(label)
    this.key = key;
    this.command = command
    this.priority = priority
  }

  compile() {
    return {
      label: this.label,
      key: this.key,
      command: this.command,
      priority: this.priority
    }
  }
}

class WaitStep extends BaseStep {
  continue_on_failure: boolean;

  constructor(label: string, continue_on_failure: boolean = false) {
    super(label)
    this.continue_on_failure = continue_on_failure;
  }

  compile() {
    return {
      label: this.label,
      continue_on_failure: this.continue_on_failure,
    }
  }
}

type Step = CommandStep | WaitStep

class Pipeline {
  steps = [];

  addSteps(...steps: Step[]) {
    this.steps = this.steps.concat(...steps)
  }

  compile() {
    return {
      steps: this.steps.map(step => step.compile())
    }
  }
}

// Actual pipeline starts here

const pipeline = new Pipeline()

pipeline.addSteps(
    new CommandStep(":go: go fmt","test-go-fmt", ".buildkite/steps/test-go-fmt.sh"),
    new WaitStep("wait",false),
    new CommandStep(":go: go build", "build", ".buildkite/steps/go-build.sh")
)

const app = new App({pipeline});

class SetPriority extends Modifier {
  run(pipeline: Pipeline): Pipeline {
    pipeline.steps = pipeline.steps.map(step => {
      if (step instanceof CommandStep) {
        step.priority = 5
      }
      return step
    })

    return pipeline
  }
}

app.apply(new SetPriority())

export default app.run();
