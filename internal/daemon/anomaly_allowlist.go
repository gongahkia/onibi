package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/web"
)

type anomalyAllowlistEntry struct {
	RuleName       string
	SessionID      string
	CreatedAt      time.Time
	EvidenceSHA256 string
}

func (d *Daemon) AddAnomalyAllowlistRule(ctx context.Context, req web.AnomalyAllowlistRequest) (string, error) {
	if d == nil || d.Registry == nil {
		return "", errors.New("session registry unavailable")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return "", errors.New("session_id required")
	}
	s, err := d.sessionByID(sessionID)
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(s.CWD)
	if root == "" {
		return "", errors.New("session cwd required")
	}
	ruleName := strings.TrimSpace(req.RuleName)
	if ruleName == "" {
		return "", errors.New("rule_name required")
	}
	sum := sha256.Sum256([]byte(req.Evidence))
	entry := anomalyAllowlistEntry{
		RuleName:       ruleName,
		SessionID:      s.ID,
		CreatedAt:      time.Now().UTC(),
		EvidenceSHA256: hex.EncodeToString(sum[:]),
	}
	path := filepath.Join(root, ".onibi", "anomaly-allowlist.toml")
	if err := appendAnomalyAllowlistEntry(path, entry); err != nil {
		return "", err
	}
	d.audit(ctx, "anomaly.allowlist.add", s.ID, "", 0, "rule="+ruleName+" path="+path)
	msg := "Allowlisted anomaly rule " + ruleName + "."
	d.publishToast(msg)
	return msg, nil
}

func appendAnomalyAllowlistEntry(path string, entry anomalyAllowlistEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		if _, err := fmt.Fprintln(f); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(f, "[[allow]]\nrule_name = %s\ncreated_at = %s\nsession_id = %s\nevidence_sha256 = %s\n",
		tomlString(entry.RuleName),
		tomlString(entry.CreatedAt.Format(time.RFC3339Nano)),
		tomlString(entry.SessionID),
		tomlString(entry.EvidenceSHA256),
	)
	return err
}

func tomlString(value string) string {
	b, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(b)
}
