package buildkite

import (
	"fmt"
)

type Data struct {
	// The key of the data
	Key string `json:"key,omitempty"`

	// The value of the data
	Value string `json:"value,omitempty"`
}

func (d Data) String() string {
	return fmt.Sprintf("Data{Key: %s, Value: %s}", d.Key, d.Value)
}

func (c *Client) DataSet(job *Job, key string, value string) (*Data, error) {
	// Create the data object we're going to send
	data := new(Data)
	data.Key = key
	data.Value = value

	// Create a new instance of a data object that will be populated
	// with the updated data by the client
	var updatedData Data

	// Return the data.
	return &updatedData, c.Post(&updatedData, "jobs/"+job.ID+"/data/set", data)
}

func (c *Client) DataGet(job *Job, key string) (*Data, error) {
	// Create the data object we're going to send
	data := new(Data)
	data.Key = key

	// Create a new instance of a data object that will be populated
	// with the updated data by the client
	var updatedData Data

	// Return the data.
	return &updatedData, c.Post(&updatedData, "jobs/"+job.ID+"/data/get", data)
}
