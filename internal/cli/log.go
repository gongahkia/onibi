package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"text/tabwriter"

	"github.com/gongahkia/onibi/internal/store"
	"github.com/spf13/cobra"
)

func runLog(cmd *cobra.Command, _ []string) error {
	n, _ := cmd.Flags().GetInt("n")
	exportPath, _ := cmd.Flags().GetString("export")
	jsonOut, _ := cmd.Flags().GetBool("json")
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
	if len(entries) == 0 {
		if jsonOut {
			return nil
		}
		cmd.Println("audit log empty")
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
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTIME\tACTION\tSESSION\tCHAT\tHASH\tDETAIL")
	for _, e := range entries {
		hash := e.PayloadHash
		if len(hash) > 12 {
			hash = hash[:12]
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d\t%s\t%s\n",
			e.ID, e.TS.Format("2006-01-02 15:04:05"), e.Action, e.SessionID, e.DecidedByChat, hash, e.Detail)
	}
	return w.Flush()
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
