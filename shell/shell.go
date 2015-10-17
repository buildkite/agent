package shell

func Run(command *Command, config *Config) (*Process, error) {
	p := &Process{Command: command, Config: config}
	err := p.Run()

	return p, err
}
