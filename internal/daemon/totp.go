package daemon

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/secrets"
)

const totpGraceWindow = 60 * time.Second

var totpNow = time.Now

func (d *Daemon) requireTOTP(ctx context.Context, chatID int64, arg string) (string, string, string, bool) {
	secret, enabled, err := d.totpSecret(ctx)
	if err != nil {
		return arg, "TOTP unavailable: " + err.Error(), "", false
	}
	if !enabled {
		return arg, "", "", true
	}
	if d.withinTOTPGrace(ctx, chatID) {
		return arg, "", "(within TOTP grace)", true
	}
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		return arg, "TOTP required: append a 6-digit code.", "", false
	}
	code := fields[len(fields)-1]
	if !isTOTPCode(code) {
		return arg, "TOTP required: append a 6-digit code.", "", false
	}
	ok, err := auth.Verify(secret, code)
	if err != nil {
		return arg, "Invalid TOTP code.", "", false
	}
	if !ok {
		return arg, "Invalid TOTP code.", "", false
	}
	d.recordTOTPSuccess(ctx, chatID)
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(arg), code)), "", "(60s grace)", true
}

func (d *Daemon) totpSecret(ctx context.Context) ([]byte, bool, error) {
	paranoid, err := d.paranoidMode(ctx)
	if err != nil {
		return nil, false, err
	}
	if d.Secrets == nil {
		if paranoid {
			return nil, true, fmt.Errorf("paranoid mode is set but no secret store is configured")
		}
		return nil, false, nil
	}
	raw, ok, err := d.Secrets.Get(secrets.KeyTOTPSecret)
	if err != nil {
		return nil, false, err
	}
	if !ok || strings.TrimSpace(raw) == "" {
		if paranoid {
			return nil, true, fmt.Errorf("paranoid mode is set but TOTP is not configured")
		}
		return nil, false, nil
	}
	secret, err := auth.DecodeHex(raw)
	if err != nil {
		return nil, true, err
	}
	return secret, true, nil
}

func (d *Daemon) totpEnabled(ctx context.Context, chatID int64) (bool, string) {
	_, enabled, err := d.totpSecret(ctx)
	if err != nil {
		return true, "TOTP unavailable: " + err.Error()
	}
	if enabled && d.withinTOTPGrace(ctx, chatID) {
		return false, ""
	}
	if enabled {
		return true, "TOTP required: use /interrupt <id> <code> or /kill <id> <code>."
	}
	return false, ""
}

func (d *Daemon) recordTOTPSuccess(ctx context.Context, chatID int64) {
	if d.DB == nil {
		return
	}
	_ = d.DB.KVSetString(ctx, totpLastKey(chatID), strconv.FormatInt(totpNow().Unix(), 10))
}

func (d *Daemon) withinTOTPGrace(ctx context.Context, chatID int64) bool {
	if d.DB == nil {
		return false
	}
	v, ok, err := d.DB.KVGetString(ctx, totpLastKey(chatID))
	if err != nil || !ok {
		return false
	}
	ts, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return false
	}
	age := totpNow().Sub(time.Unix(ts, 0))
	return age >= 0 && age < totpGraceWindow
}

func totpLastKey(chatID int64) string {
	return "totp:last:" + strconv.FormatInt(chatID, 10)
}

func withTOTPNote(text, note string) string {
	if note == "" {
		return text
	}
	return text + " " + note
}

func (d *Daemon) paranoidMode(ctx context.Context) (bool, error) {
	if d.DB == nil {
		return false, nil
	}
	v, ok, err := d.DB.KVGetString(ctx, "paranoid")
	if err != nil {
		return false, err
	}
	return ok && v == "1", nil
}

func isTOTPCode(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
