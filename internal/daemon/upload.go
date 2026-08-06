package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (d *Daemon) StageUpload(ctx context.Context, sessionID, name string, expectedSize int64, source io.Reader) (string, time.Time, error) {
	if source == nil {
		return "", time.Time{}, errors.New("upload source required")
	}
	if _, err := d.sessionForRPCTarget(sessionID); err != nil {
		return "", time.Time{}, err
	}
	if expectedSize < 0 || expectedSize > d.UploadMaxBytes {
		return "", time.Time{}, fmt.Errorf("upload exceeds %d byte limit", d.UploadMaxBytes)
	}
	dir := filepath.Join(d.Paths.StateDir, "uploads", sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", time.Time{}, err
	}
	name = safeUploadName(name)
	path := filepath.Join(dir, NewID()+"-"+name)
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", time.Time{}, err
	}
	limited := io.LimitReader(source, d.UploadMaxBytes+1)
	n, copyErr := io.Copy(file, limited)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || n > d.UploadMaxBytes {
		_ = os.Remove(tmp)
		if n > d.UploadMaxBytes {
			return "", time.Time{}, fmt.Errorf("upload exceeds %d byte limit", d.UploadMaxBytes)
		}
		if copyErr != nil {
			return "", time.Time{}, copyErr
		}
		return "", time.Time{}, closeErr
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", time.Time{}, err
	}
	expires := time.Now().Add(d.UploadTTL)
	return path, expires, nil
}

func (d *Daemon) PurgeExpiredUploads(now time.Time) error {
	root := filepath.Join(d.Paths.StateDir, "uploads")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	cutoff := now.Add(-d.UploadTTL)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			path := filepath.Join(dir, file.Name())
			info, err := file.Info()
			if err == nil && info.ModTime().Before(cutoff) {
				_ = os.Remove(path)
			}
		}
		_ = os.Remove(dir)
	}
	return nil
}

func (d *Daemon) sweepUploads(ctx context.Context) {
	d.purgeTransientState(ctx, time.Now())
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			d.purgeTransientState(ctx, now)
		}
	}
}

func (d *Daemon) purgeTransientState(ctx context.Context, now time.Time) {
	if err := d.PurgeExpiredUploads(now); err != nil {
		d.Log.Warn("purge uploads", "err", err)
	}
	if d.DB != nil {
		if err := d.DB.TelegramPurge(ctx, now.Add(-7*24*time.Hour)); err != nil {
			d.Log.Warn("purge Telegram state", "err", err)
		}
	}
}

func safeUploadName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" {
		return "upload"
	}
	var out strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			out.WriteRune(r)
		} else {
			out.WriteByte('_')
		}
		if out.Len() >= 96 {
			break
		}
	}
	if out.Len() == 0 {
		return "upload"
	}
	return out.String()
}
