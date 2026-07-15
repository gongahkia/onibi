package fleet

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxBudgetSessions = 256

type BudgetReport struct {
	Date        string          `json:"date,omitempty"`
	DailyTokens int64           `json:"daily_tokens,omitempty"`
	GlobalLimit int64           `json:"global_limit,omitempty"`
	OnOverrun   string          `json:"on_overrun,omitempty"`
	Sessions    []BudgetSession `json:"sessions,omitempty"`
}

type BudgetSession struct {
	SessionID string `json:"session_id"`
	Agent     string `json:"agent"`
	Tokens    int64  `json:"tokens"`
	Limit     int64  `json:"limit,omitempty"`
	OnOverrun string `json:"on_overrun,omitempty"`
	Measured  bool   `json:"measured"`
}

func (r BudgetReport) Empty() bool {
	return r.Date == "" && r.DailyTokens == 0 && r.GlobalLimit == 0 && r.OnOverrun == "" && len(r.Sessions) == 0
}

func (r BudgetReport) Validate() error {
	if r.Empty() {
		return nil
	}
	if _, err := time.Parse("2006-01-02", r.Date); err != nil || r.DailyTokens < 0 || r.GlobalLimit < 0 || !validBudgetAction(r.OnOverrun) {
		return errors.New("invalid fleet budget report")
	}
	if len(r.Sessions) > maxBudgetSessions {
		return errors.New("too many fleet budget sessions")
	}
	seen := make(map[string]bool, len(r.Sessions))
	for _, session := range r.Sessions {
		if !validID(session.SessionID) || !validBudgetAgent(session.Agent) || session.Tokens < 0 || session.Limit < 0 || !validBudgetAction(session.OnOverrun) || seen[session.SessionID] {
			return errors.New("invalid fleet budget session")
		}
		seen[session.SessionID] = true
	}
	return nil
}

func (r BudgetReport) Normalized() BudgetReport {
	if r.Empty() {
		return BudgetReport{}
	}
	r.Date = strings.TrimSpace(r.Date)
	r.OnOverrun = normalizeBudgetAction(r.OnOverrun)
	r.Sessions = append([]BudgetSession(nil), r.Sessions...)
	for i := range r.Sessions {
		r.Sessions[i].SessionID = strings.TrimSpace(r.Sessions[i].SessionID)
		r.Sessions[i].Agent = strings.ToLower(strings.TrimSpace(r.Sessions[i].Agent))
		r.Sessions[i].OnOverrun = normalizeBudgetAction(r.Sessions[i].OnOverrun)
	}
	sort.Slice(r.Sessions, func(i, j int) bool { return r.Sessions[i].SessionID < r.Sessions[j].SessionID })
	return r
}

func BudgetReportSigningPayload(r BudgetReport) string {
	r = r.Normalized()
	rows := make([]string, 0, len(r.Sessions))
	for _, session := range r.Sessions {
		rows = append(rows, strings.Join([]string{session.SessionID, session.Agent, strconv.FormatInt(session.Tokens, 10), strconv.FormatInt(session.Limit, 10), session.OnOverrun, strconv.FormatBool(session.Measured)}, ","))
	}
	return strings.Join([]string{r.Date, strconv.FormatInt(r.DailyTokens, 10), strconv.FormatInt(r.GlobalLimit, 10), r.OnOverrun, strings.Join(rows, ";")}, "\n")
}

func validBudgetAgent(agent string) bool {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "claude", "codex", "pi":
		return true
	default:
		return false
	}
}

func validBudgetAction(action string) bool {
	switch normalizeBudgetAction(action) {
	case "warn", "interrupt", "kill":
		return true
	default:
		return false
	}
}

func normalizeBudgetAction(action string) string {
	if strings.TrimSpace(action) == "" {
		return "interrupt"
	}
	return strings.ToLower(strings.TrimSpace(action))
}
