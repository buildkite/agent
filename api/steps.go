package api

// StepUpdate represents a change request to a step
type StepUpdate struct {
	UUID      string `json:"uuid,omitempty"`
	Attribute string `json:"attribute,omitempty"`
	Value     string `json:"value,omitempty"`
	Append    bool   `json:"append,omitempty"`
}
