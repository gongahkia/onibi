package cli

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gongahkia/onibi/internal/store"
	"github.com/spf13/cobra"
)

func runLog(cmd *cobra.Command, _ []string) error {
	n, _ := cmd.Flags().GetInt("n")
	exportPath, _ := cmd.Flags().GetString("export")
	jsonOut, _ := cmd.Flags().GetBool("json")
	notifyOnly, _ := cmd.Flags().GetBool("notify")
	db, err := openDefaultDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if exportPath != "" {
		entries, err := db.AuditAll(cmd.Context())
		if err != nil {
			return err
		}
		return exportAudit(exportPath, entries)
	}
	entries, err := db.AuditRecent(cmd.Context(), n)
	if err != nil {
		return err
	}
	if notifyOnly {
		entries = filterNotifyAudit(entries)
	}
	if len(entries) == 0 {
		if jsonOut {
			return nil
		}
		cmd.Println(styleFor(cmd).dim("audit log empty"))
		return nil
	}
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		for _, e := range entries {
			if err := enc.Encode(e); err != nil {
				return err
			}
		}
		return nil
	}
	style := styleFor(cmd)
	table := [][]string{tableHeader(style, "ID", "TIME", "ACTION", "SESSION", "CHAT", "HASH", "DETAIL")}
	for _, e := range entries {
		hash := e.PayloadHash
		if len(hash) > 12 {
			hash = hash[:12]
		}
		table = append(table, []string{
			strconv.FormatInt(e.ID, 10),
			e.TS.Format("2006-01-02 15:04:05"),
			styleAuditAction(style, e.Action),
			e.SessionID,
			strconv.FormatInt(e.DecidedByChat, 10),
			hash,
			e.Detail,
		})
	}
	return renderTable(cmd.OutOrStdout(), table)
}

func exportAudit(path string, entries []store.AuditEntry) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	switch filepath.Ext(path) {
	case ".csv":
		w := csv.NewWriter(f)
		if err := w.Write([]string{"id", "ts", "action", "session_id", "payload_hash", "decided_by_chat", "detail"}); err != nil {
			return err
		}
		for _, e := range entries {
			if err := w.Write([]string{
				strconv.FormatInt(e.ID, 10),
				e.TS.Format("2006-01-02T15:04:05Z07:00"),
				e.Action,
				e.SessionID,
				e.PayloadHash,
				strconv.FormatInt(e.DecidedByChat, 10),
				e.Detail,
			}); err != nil {
				return err
			}
		}
		w.Flush()
		return w.Error()
	default:
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}
}

func styleAuditAction(style cliStyle, action string) string {
	switch {
	case strings.Contains(action, ".failed"):
		return style.red(action)
	case strings.HasPrefix(action, "notify.") && strings.Contains(action, ".error"):
		return style.red(action)
	case strings.Contains(action, ".expired"), strings.Contains(action, ".stale"):
		return style.yellow(action)
	case strings.HasPrefix(action, "notify."):
		return style.green(action)
	case strings.Contains(action, ".start"), strings.Contains(action, ".sent"), strings.Contains(action, ".decided"):
		return style.green(action)
	default:
		return action
	}
}

func filterNotifyAudit(entries []store.AuditEntry) []store.AuditEntry {
	out := entries[:0]
	for _, e := range entries {
		if strings.HasPrefix(e.Action, "notify.") {
			out = append(out, e)
		}
	}
	return out
}
