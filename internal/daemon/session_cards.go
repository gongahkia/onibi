package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type SessionCard struct {
	ID        string
	ShortID   string
	Name      string
	Agent     string
	Project   string
	Mode      string
	State     string
	Age       time.Duration
	Last      time.Duration
	Queued    int
	Approvals int
	Default   bool
}

func (d *Daemon) sessionCards(ctx context.Context, chatID int64, sessions []*Session) []SessionCard {
	defaultID := d.activeDefaultTarget(ctx, chatID)
	if defaultID == "" && len(sessions) == 1 {
		defaultID = sessions[0].ID
	}
	out := make([]SessionCard, 0, len(sessions))
	for _, s := range sessions {
		card := SessionCard{
			ID:        s.ID,
			ShortID:   shortID(s.ID),
			Name:      compactValue(s.Name, 32),
			Agent:     compactValue(s.Agent, 18),
			Project:   compactValue(d.projectLabel(ctx, s.CWD), 28),
			Mode:      compactValue(d.sessionMode(ctx, s), 18),
			State:     d.sessionState(s),
			Age:       time.Since(s.StartedAt()).Truncate(time.Second),
			Last:      time.Since(s.LastActivityAt()).Truncate(time.Second),
			Queued:    d.queuedPromptCountForSession(ctx, s.ID),
			Approvals: d.pendingApprovalCountForSession(ctx, s.ID),
			Default:   s.ID == defaultID,
		}
		out = append(out, card)
	}
	return out
}

func (d *Daemon) sessionCardsText(ctx context.Context, chatID int64, sessions []*Session) string {
	cards := d.sessionCards(ctx, chatID, sessions)
	var b strings.Builder
	for i, c := range cards {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(c.Text())
	}
	return b.String()
}

func (c SessionCard) Text() string {
	mark := " "
	if c.Default {
		mark = "*"
	}
	return fmt.Sprintf("%s %s (%s)\n  agent=%s project=%s mode=%s state=%s\n  age=%s last=%s queue=%d approvals=%d",
		mark, c.Name, c.ShortID, c.Agent, c.Project, c.Mode, c.State, c.Age, c.Last, c.Queued, c.Approvals)
}

func (c SessionCard) Label() string {
	prefix := ""
	if c.Default {
		prefix = "* "
	}
	return compactValue(fmt.Sprintf("%s%s %s %s %s", prefix, c.Name, c.Agent, c.Project, c.ShortID), 80)
}

func (d *Daemon) projectLabel(ctx context.Context, cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "unknown"
	}
	clean := filepath.Clean(cwd)
	for _, key := range d.mustProjectAliasKeys(ctx) {
		alias := strings.TrimPrefix(key, projectAliasPrefix)
		path, ok, err := d.DB.KVGetString(ctx, key)
		if err == nil && ok && filepath.Clean(path) == clean {
			return alias
		}
	}
	base := filepath.Base(clean)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "cwd"
	}
	return base
}

func (d *Daemon) mustProjectAliasKeys(ctx context.Context) []string {
	if d.DB == nil {
		return nil
	}
	keys, err := d.projectAliasKeys(ctx)
	if err != nil {
		return nil
	}
	return keys
}

func (d *Daemon) queuedPromptCountForSession(ctx context.Context, sessionID string) int {
	if d.DB == nil {
		return 0
	}
	rows, err := d.DB.PromptList(ctx, sessionID, false, 1000)
	if err != nil {
		return 0
	}
	return len(rows)
}

func (d *Daemon) pendingApprovalCountForSession(ctx context.Context, sessionID string) int {
	if d.Queue == nil {
		return 0
	}
	pending, err := d.Queue.Pending(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, a := range pending {
		if a.SessionID == sessionID {
			n++
		}
	}
	return n
}

func compactValue(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if limit <= 0 || len(runes) <= limit {
		return s
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}
