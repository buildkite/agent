package buildkite

type MetaData struct {
	// The ID of the Job
	JobID string

	// The key of the data
	Key string `json:"key,omitempty"`

	// The value of the data
	Value string `json:"value,omitempty"`

	// The API used for communication
	API API
}

func (d *MetaData) Set() error {
	return d.API.Post("jobs/"+d.JobID+"/data/set", &d, d)
}

func (d *MetaData) Get() error {
	return d.API.Post("jobs/"+d.JobID+"/data/get", &d, d)
}
