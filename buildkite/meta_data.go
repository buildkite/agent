package buildkite

type MetaData struct {
	API   API
	JobID string
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

func (d *MetaData) Set() error {
	return d.API.Post("jobs/"+d.JobID+"/data/set", &d, d)
}

func (d *MetaData) Get() error {
	return d.API.Post("jobs/"+d.JobID+"/data/get", &d, d)
}
