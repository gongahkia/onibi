package approval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const ApprovalSchemaV1 = "onibi.approval.v1"

type Request struct {
	Version   string          `json:"version"`
	SessionID string          `json:"session_id"`
	Agent     string          `json:"agent"`
	Tool      string          `json:"tool"`
	Input     json.RawMessage `json:"input"`
	Details   Details         `json:"details"`
	Risk      Risk            `json:"risk"`
}

func NormalizeRequest(req Request) (Request, error) {
	if req.Version != "" && req.Version != ApprovalSchemaV1 {
		return Request{}, fmt.Errorf("unsupported approval schema %q", req.Version)
	}
	req.Version = ApprovalSchemaV1
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Agent = strings.TrimSpace(req.Agent)
	req.Tool = strings.TrimSpace(req.Tool)
	if req.SessionID == "" || req.Agent == "" || req.Tool == "" {
		return Request{}, fmt.Errorf("approval session_id, agent, and tool are required")
	}
	input := bytes.TrimSpace(req.Input)
	if len(input) == 0 {
		input = []byte(`{}`)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input, &object); err != nil || object == nil {
		return Request{}, fmt.Errorf("approval input must be a JSON object")
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return Request{}, fmt.Errorf("canonicalize approval input: %w", err)
	}
	req.Input = canonical
	req.Details = ExtractDetails(req.Tool, string(canonical))
	req.Risk = ClassifyRisk(req.Tool, string(canonical))
	return req, nil
}

func RequestForApproval(a Approval) (Request, error) {
	return NormalizeRequest(Request{
		Version:   ApprovalSchemaV1,
		SessionID: a.SessionID,
		Agent:     a.Agent,
		Tool:      a.Tool,
		Input:     json.RawMessage(a.InputJSON),
	})
}

type Payload struct {
	Version       string         `json:"version"`
	ID            string         `json:"id"`
	SessionID     string         `json:"session_id"`
	Agent         string         `json:"agent"`
	Tool          string         `json:"tool"`
	State         string         `json:"state"`
	ScrubbedInput string         `json:"scrubbed_input"`
	Details       Details        `json:"details"`
	Risk          Risk           `json:"risk"`
	UnifiedDiff   string         `json:"unified_diff,omitempty"`
	BudgetWarn    *BudgetWarning `json:"budget_warning,omitempty"`
}

func PayloadForApproval(a Approval) (Payload, error) {
	req, err := RequestForApproval(a)
	if err != nil {
		return Payload{}, err
	}
	return Payload{
		Version:       req.Version,
		ID:            a.ID,
		SessionID:     req.SessionID,
		Agent:         req.Agent,
		Tool:          req.Tool,
		State:         a.State,
		ScrubbedInput: Scrub(string(req.Input)),
		Details: Details{
			Target:   Scrub(req.Details.Target),
			Command:  Scrub(req.Details.Command),
			FilePath: Scrub(req.Details.FilePath),
		},
		Risk:        req.Risk,
		UnifiedDiff: a.UnifiedDiff,
		BudgetWarn:  cloneBudgetWarning(a.BudgetWarn),
	}, nil
}
