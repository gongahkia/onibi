package budget

type View struct {
	Sessions []SessionUsage `json:"sessions"`
	Daily    DailyUsage     `json:"daily"`
}

type SessionUsage struct {
	SessionID         string   `json:"session_id"`
	Name              string   `json:"name,omitempty"`
	Agent             string   `json:"agent,omitempty"`
	Model             string   `json:"model,omitempty"`
	InputTokens       int64    `json:"input_tokens"`
	OutputTokens      int64    `json:"output_tokens"`
	TotalInputTokens  int64    `json:"total_input_tokens"`
	TotalOutputTokens int64    `json:"total_output_tokens"`
	TotalTokens       int64    `json:"total_tokens"`
	TotalUSD          float64  `json:"total_usd,omitempty"`
	CostKnown         bool     `json:"cost_known"`
	LimitTokens       *int64   `json:"limit_tokens,omitempty"`
	RemainingTokens   *int64   `json:"remaining_tokens,omitempty"`
	RemainingUSD      *float64 `json:"remaining_usd,omitempty"`
	OnOverrun         string   `json:"on_overrun,omitempty"`
	UpdatedAt         string   `json:"updated_at,omitempty"`
}

type DailyUsage struct {
	Date            string   `json:"date"`
	TotalTokens     int64    `json:"total_tokens"`
	TotalUSD        float64  `json:"total_usd,omitempty"`
	CostKnown       bool     `json:"cost_known"`
	LimitTokens     *int64   `json:"limit_tokens,omitempty"`
	RemainingTokens *int64   `json:"remaining_tokens,omitempty"`
	RemainingUSD    *float64 `json:"remaining_usd,omitempty"`
}
