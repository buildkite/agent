package api

import (
	"context"
	"fmt"
)

// ReusePlan is the control plane's step reuse plan for a job: which upstream
// outputs to restore before the command runs, and whether and what to save
// afterwards.
type ReusePlan struct {
	StepKey            string         `json:"step_key"`
	Save               bool           `json:"save"`
	SaveDisabledReason *string        `json:"save_disabled_reason"`
	Files              *ReuseFiles    `json:"files"` // nil when the step has no inputs.files
	Outputs            ReuseOutputs   `json:"outputs"`
	Restore            []ReuseRestore `json:"restore"`

	// Values are the output values this job has set with `output set`.
	Values map[string]string `json:"values"`
}

// ReuseFiles is a step's parsed `inputs.files`.
type ReuseFiles struct {
	Include   []string `json:"include"`
	Exclude   []string `json:"exclude"`
	Untracked bool     `json:"untracked"`
}

// ReuseOutputs is a step's declared `outputs`.
type ReuseOutputs struct {
	Paths     []string `json:"paths"`
	Artifacts []string `json:"artifacts"`
	Values    []string `json:"values"`
}

// ReuseRestore lists the outputs of an upstream step to restore.
type ReuseRestore struct {
	StepKey  string      `json:"step_key"`
	Download bool        `json:"download"` // download the step's artifacts too
	Paths    []ReusePath `json:"paths"`

	// Signature is the signed result manifest of the upstream job, if it had
	// a signing key.
	Signature *string `json:"signature"`
}

// ReusePath is a stored output path.
type ReusePath struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
	Key    string `json:"key"` // blob store key of the archive
	Size   int64  `json:"size"`
	Files  int    `json:"files"`
}

// ReuseResult reports the outcome of a job that may be saved for reuse.
type ReuseResult struct {
	ExitStatus          int         `json:"exit_status"`
	VerifiedFilesDigest *string     `json:"verified_files_digest"`
	Paths               []ReusePath `json:"paths"`
	OverheadMS          int64       `json:"overhead_ms"`
	DiscardReason       *string     `json:"discard_reason"`

	// Signature is the signed result manifest, if the job has a signing key.
	Signature *string `json:"signature"`
}

// ReuseResultResponse is the control plane's response to a ReuseResult.
type ReuseResultResponse struct {
	Saved     bool   `json:"saved"`
	Reason    string `json:"reason"`
	ActionKey string `json:"action_key"`
}

// ReuseInputUnavailable reports that an upstream output could not be restored.
type ReuseInputUnavailable struct {
	StepKey string `json:"step_key"`
	Path    string `json:"path"`
	Reason  string `json:"reason"`
}

// ReuseInputUnavailableResponse is the control plane's response to a
// ReuseInputUnavailable report.
type ReuseInputUnavailableResponse struct {
	Repair  bool   `json:"repair"`
	Message string `json:"message"`
}

// OutputValue is a named output value of a step.
type OutputValue struct {
	Name  string `json:"name,omitempty"`
	Value string `json:"value"`
}

// GetReusePlan gets the step reuse plan for a job.
func (c *Client) GetReusePlan(ctx context.Context, jobID string) (*ReusePlan, *Response, error) {
	u := fmt.Sprintf("jobs/%s/reuse", railsPathEscape(jobID))

	req, err := c.newRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, nil, err
	}

	plan := new(ReusePlan)
	resp, err := c.doRequest(req, plan)
	if err != nil {
		return nil, resp, err
	}

	return plan, resp, nil
}

// SubmitReuseResult reports the outcome of a job for step reuse.
func (c *Client) SubmitReuseResult(ctx context.Context, jobID string, result *ReuseResult) (*ReuseResultResponse, *Response, error) {
	u := fmt.Sprintf("jobs/%s/reuse/result", railsPathEscape(jobID))

	req, err := c.newRequest(ctx, "POST", u, result)
	if err != nil {
		return nil, nil, err
	}

	r := new(ReuseResultResponse)
	resp, err := c.doRequest(req, r)
	if err != nil {
		return nil, resp, err
	}

	return r, resp, nil
}

// ReportReuseInputUnavailable reports that an upstream output could not be
// restored for a job.
func (c *Client) ReportReuseInputUnavailable(ctx context.Context, jobID string, report *ReuseInputUnavailable) (*ReuseInputUnavailableResponse, *Response, error) {
	u := fmt.Sprintf("jobs/%s/reuse/input_unavailable", railsPathEscape(jobID))

	req, err := c.newRequest(ctx, "POST", u, report)
	if err != nil {
		return nil, nil, err
	}

	r := new(ReuseInputUnavailableResponse)
	resp, err := c.doRequest(req, r)
	if err != nil {
		return nil, resp, err
	}

	return r, resp, nil
}

// SetOutputValue sets a named output value of the job's step.
func (c *Client) SetOutputValue(ctx context.Context, jobID string, value *OutputValue) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/outputs/values", railsPathEscape(jobID))

	req, err := c.newRequest(ctx, "PUT", u, value)
	if err != nil {
		return nil, err
	}

	return c.doRequest(req, nil)
}

// GetOutputValue gets a named output value of a step in the job's build. The
// response has status 404 when the value is unset.
func (c *Client) GetOutputValue(ctx context.Context, jobID, stepKey, name string) (*OutputValue, *Response, error) {
	u := fmt.Sprintf("jobs/%s/outputs/values", railsPathEscape(jobID))

	req, err := c.newRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, nil, err
	}

	q := req.URL.Query()
	q.Set("step", stepKey)
	q.Set("name", name)
	req.URL.RawQuery = q.Encode()

	v := new(OutputValue)
	resp, err := c.doRequest(req, v)
	if err != nil {
		return nil, resp, err
	}

	return v, resp, nil
}
