package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/secrets"
)

func (d *Daemon) requireTOTP(ctx context.Context, arg string) (string, string, bool) {
	secret, enabled, err := d.totpSecret(ctx)
	if err != nil {
		return arg, "TOTP unavailable: " + err.Error(), false
	}
	if !enabled {
		return arg, "", true
	}
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		return arg, "TOTP required: append a 6-digit code.", false
	}
	code := fields[len(fields)-1]
	if !isTOTPCode(code) {
		return arg, "TOTP required: append a 6-digit code.", false
	}
	ok, err := auth.Verify(secret, code)
	if err != nil {
		return arg, "Invalid TOTP code.", false
	}
	if !ok {
		return arg, "Invalid TOTP code.", false
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(arg), code)), "", true
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

func (d *Daemon) totpEnabled(ctx context.Context) (bool, string) {
	_, enabled, err := d.totpSecret(ctx)
	if err != nil {
		return true, "TOTP unavailable: " + err.Error()
	}
	if enabled {
		return true, "TOTP required: use /interrupt <id> <code> or /kill <id> <code>."
	}
	return false, ""
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
