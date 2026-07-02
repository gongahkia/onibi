package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var profileNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type Profile struct {
	Name       string
	Transport  string
	Agent      string
	Workspace  string
	CWD        string
	LastUsedAt time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type profilePayload struct {
	Transport string `json:"transport,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	CWD       string `json:"cwd,omitempty"`
}

func (d *DB) ProfileUpsert(ctx context.Context, profile Profile) error {
	profile, err := d.normalizeProfile(ctx, profile)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(profilePayload{
		Transport: profile.Transport,
		Agent:     profile.Agent,
		Workspace: profile.Workspace,
		CWD:       profile.CWD,
	})
	if err != nil {
		return err
	}
	sealed, err := d.sealProfilePayload(ctx, profile.Name, payload)
	if err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx,
		`INSERT INTO profiles(name, data_enc, last_used_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   data_enc=excluded.data_enc,
		   last_used_at=excluded.last_used_at,
		   updated_at=excluded.updated_at`,
		profile.Name,
		sealed,
		unixOrZero(profile.LastUsedAt),
		unixOrZero(profile.CreatedAt),
		unixOrZero(profile.UpdatedAt),
	)
	return err
}

func (d *DB) ProfileGet(ctx context.Context, name string) (Profile, bool, error) {
	name = strings.TrimSpace(name)
	if err := validateProfileName(name); err != nil {
		return Profile{}, false, err
	}
	row := d.sql.QueryRowContext(ctx,
		`SELECT name, data_enc, last_used_at, created_at, updated_at
		   FROM profiles WHERE name = ?`, name)
	profile, err := d.scanProfile(ctx, row)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, false, nil
	}
	if err != nil {
		return Profile{}, false, err
	}
	return profile, true, nil
}

func (d *DB) ProfileList(ctx context.Context) ([]Profile, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT name, data_enc, last_used_at, created_at, updated_at
		   FROM profiles ORDER BY last_used_at DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Profile
	for rows.Next() {
		profile, err := d.scanProfile(ctx, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, profile)
	}
	return out, rows.Err()
}

func (d *DB) ProfileRemove(ctx context.Context, name string) (bool, error) {
	name = strings.TrimSpace(name)
	if err := validateProfileName(name); err != nil {
		return false, err
	}
	res, err := d.sql.ExecContext(ctx, `DELETE FROM profiles WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (d *DB) ProfileTouch(ctx context.Context, name string, now time.Time) error {
	name = strings.TrimSpace(name)
	if err := validateProfileName(name); err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	res, err := d.sql.ExecContext(ctx,
		`UPDATE profiles SET last_used_at = ?, updated_at = ? WHERE name = ?`,
		now.UTC().Unix(), now.UTC().Unix(), name)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("profile %q not found", name)
	}
	return nil
}

type profileScanner interface {
	Scan(dest ...any) error
}

func (d *DB) scanProfile(ctx context.Context, row profileScanner) (Profile, error) {
	var profile Profile
	var sealed []byte
	var lastUsedAt, createdAt, updatedAt int64
	if err := row.Scan(&profile.Name, &sealed, &lastUsedAt, &createdAt, &updatedAt); err != nil {
		return Profile{}, err
	}
	opened, err := d.openProfilePayload(ctx, profile.Name, sealed)
	if err != nil {
		return Profile{}, err
	}
	var payload profilePayload
	if err := json.Unmarshal(opened, &payload); err != nil {
		return Profile{}, err
	}
	profile.Transport = payload.Transport
	profile.Agent = payload.Agent
	profile.Workspace = payload.Workspace
	profile.CWD = payload.CWD
	profile.LastUsedAt = timeOrZero(lastUsedAt)
	profile.CreatedAt = timeOrZero(createdAt)
	profile.UpdatedAt = timeOrZero(updatedAt)
	return profile, nil
}

func (d *DB) normalizeProfile(ctx context.Context, profile Profile) (Profile, error) {
	profile.Name = strings.TrimSpace(profile.Name)
	if err := validateProfileName(profile.Name); err != nil {
		return Profile{}, err
	}
	profile.Transport = strings.ToLower(strings.TrimSpace(profile.Transport))
	profile.Agent = strings.TrimSpace(profile.Agent)
	profile.Workspace = strings.TrimSpace(profile.Workspace)
	profile.CWD = strings.TrimSpace(profile.CWD)
	now := time.Now().UTC()
	if existing, ok, err := d.ProfileGet(ctx, profile.Name); err != nil {
		return Profile{}, err
	} else if ok {
		if profile.CreatedAt.IsZero() {
			profile.CreatedAt = existing.CreatedAt
		}
		if profile.LastUsedAt.IsZero() {
			profile.LastUsedAt = existing.LastUsedAt
		}
	}
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = now
	}
	if profile.UpdatedAt.IsZero() {
		profile.UpdatedAt = now
	}
	return profile, nil
}

func (d *DB) sealProfilePayload(ctx context.Context, name string, payload []byte) ([]byte, error) {
	if d.cryptbox == nil {
		return nil, ErrCryptBoxUnavailable
	}
	return d.cryptbox.Seal(ctx, payload, RowAAD("profiles", name, "data_enc"))
}

func (d *DB) openProfilePayload(ctx context.Context, name string, sealed []byte) ([]byte, error) {
	if d.cryptbox == nil {
		return nil, ErrCryptBoxUnavailable
	}
	return d.cryptbox.Open(ctx, sealed, RowAAD("profiles", name, "data_enc"))
}

func validateProfileName(name string) error {
	if !profileNamePattern.MatchString(strings.TrimSpace(name)) {
		return fmt.Errorf("invalid profile name %q", name)
	}
	return nil
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().Unix()
}

func timeOrZero(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Unix(v, 0).UTC()
}
