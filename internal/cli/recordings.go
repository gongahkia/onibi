package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/spf13/cobra"
)

var recordingDefaultPaths = config.DefaultPaths

type recordingEntry struct {
	SessionID string
	Name      string
	Path      string
	Size      int64
	ModTime   time.Time
}

func runRecordingsList(cmd *cobra.Command, _ []string) error {
	entries, err := recordingEntries()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		cmd.Println("no recordings")
		return nil
	}
	style := styleFor(cmd)
	table := [][]string{tableHeader(style, "SESSION", "FILE", "SIZE", "MODIFIED")}
	for _, entry := range entries {
		table = append(table, []string{
			entry.SessionID,
			entry.Name,
			formatBytes(entry.Size),
			ago(entry.ModTime),
		})
	}
	return renderTable(cmd.OutOrStdout(), table)
}

func runRecordingsExport(cmd *cobra.Command, args []string) error {
	src, err := currentRecordingPath(args[0])
	if err != nil {
		return err
	}
	dst := args[1]
	if info, err := os.Stat(dst); err == nil && info.IsDir() {
		dst = filepath.Join(dst, filepath.Base(src))
	}
	if err := copyFile(dst, src); err != nil {
		return err
	}
	cmd.Println("exported " + dst)
	return nil
}

func runRecordingsDelete(cmd *cobra.Command, args []string) error {
	paths, err := recordingPathsForSession(args[0])
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errors.New("recording not found")
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	cmd.Printf("deleted %d recording(s)\n", len(paths))
	return nil
}

func recordingEntries() ([]recordingEntry, error) {
	dir, err := recordingsDir()
	if err != nil {
		return nil, err
	}
	rows, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries := make([]recordingEntry, 0, len(rows))
	for _, row := range rows {
		if row.IsDir() || !strings.HasSuffix(row.Name(), ".cast") {
			continue
		}
		info, err := row.Info()
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, row.Name())
		entries = append(entries, recordingEntry{
			SessionID: recordingSessionID(row.Name()),
			Name:      row.Name(),
			Path:      path,
			Size:      info.Size(),
			ModTime:   info.ModTime(),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ModTime.After(entries[j].ModTime)
	})
	return entries, nil
}

func currentRecordingPath(sessionID string) (string, error) {
	dir, err := recordingsDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, safeRecordingName(sessionID)+".cast")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errors.New("recording not found")
		}
		return "", err
	}
	if info.IsDir() {
		return "", errors.New("recording not found")
	}
	return path, nil
}

func recordingPathsForSession(sessionID string) ([]string, error) {
	dir, err := recordingsDir()
	if err != nil {
		return nil, err
	}
	base := safeRecordingName(sessionID)
	matches, err := filepath.Glob(filepath.Join(dir, base+"-*.cast"))
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, base+".cast")); err == nil {
		matches = append(matches, filepath.Join(dir, base+".cast"))
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

func recordingsDir() (string, error) {
	paths, err := recordingDefaultPaths()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(paths.StateDir) == "" {
		return "", errors.New("state dir unavailable")
	}
	return filepath.Join(paths.StateDir, "recordings"), nil
}

func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func safeRecordingName(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "session"
	}
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, sessionID)
}

func recordingSessionID(name string) string {
	base := strings.TrimSuffix(name, ".cast")
	if idx := strings.Index(base, "-"); idx > 0 {
		return base[:idx]
	}
	return base
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
