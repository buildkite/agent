package logger

type Field interface {
	Key() string
	Value() string
}

type Fields []Field

func (f *Fields) Add(fields ...Field) {
	*f = append(*f, fields...)
}

func (f *Fields) Get(key string) []Field {
	fields := []Field{}
	for _, field := range *f {
		if field.Key() == key {
			fields = append(fields, field)
		}
	}
	return fields
}

type agentNameField string

func (a agentNameField) Key() string {
	return `agent_name`
}

func (a agentNameField) Value() string {
	return string(a)
}

func AgentNameField(name string) Field {
	return agentNameField(name)
}
