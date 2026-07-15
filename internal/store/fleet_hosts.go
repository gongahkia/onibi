package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

func (d *DB) FleetHostUpsert(ctx context.Context, host fleet.Host) error {
	host = host.Normalized()
	if err := host.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(host)
	if err != nil {
		return err
	}
	sealed, err := d.sealFleetHost(ctx, host.ID, payload)
	if err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx,
		`INSERT INTO fleet_hosts(id, data_enc, state, registered_at, last_seen_at, revoked_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   data_enc=excluded.data_enc,
		   state=excluded.state,
		   registered_at=excluded.registered_at,
		   last_seen_at=excluded.last_seen_at,
		   revoked_at=excluded.revoked_at,
		   updated_at=excluded.updated_at`,
		host.ID,
		sealed,
		string(host.State),
		unixOrZero(host.RegisteredAt),
		unixOrZero(host.LastSeenAt),
		unixOrZero(revokedAt(host.RevokedAt)),
		time.Now().UTC().Unix(),
	)
	return err
}

func (d *DB) FleetHostGet(ctx context.Context, id string) (fleet.Host, bool, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT id, data_enc FROM fleet_hosts WHERE id = ?`, id)
	host, err := d.scanFleetHost(ctx, row)
	if errors.Is(err, sql.ErrNoRows) {
		return fleet.Host{}, false, nil
	}
	if err != nil {
		return fleet.Host{}, false, err
	}
	return host, true, nil
}

func (d *DB) FleetHostList(ctx context.Context) ([]fleet.Host, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id, data_enc FROM fleet_hosts ORDER BY registered_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]fleet.Host, 0)
	for rows.Next() {
		host, err := d.scanFleetHost(ctx, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, host)
	}
	return out, rows.Err()
}

func (d *DB) FleetHostRotateIdentity(ctx context.Context, hostID, currentIdentityPublic, newIdentityPublic string) (fleet.Host, bool, error) {
	if strings.TrimSpace(hostID) == "" || strings.TrimSpace(currentIdentityPublic) == "" || strings.TrimSpace(newIdentityPublic) == "" || currentIdentityPublic == newIdentityPublic {
		return fleet.Host{}, false, errors.New("invalid fleet host identity rotation")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fleet.Host{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var sealed []byte
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT data_enc, state FROM fleet_hosts WHERE id = ?`, hostID).Scan(&sealed, &state); errors.Is(err, sql.ErrNoRows) {
		return fleet.Host{}, false, nil
	} else if err != nil {
		return fleet.Host{}, false, err
	}
	if state != string(fleet.HostStateActive) {
		return fleet.Host{}, false, nil
	}
	payload, err := d.openFleetHost(ctx, hostID, sealed)
	if err != nil {
		return fleet.Host{}, false, err
	}
	var host fleet.Host
	if err := json.Unmarshal(payload, &host); err != nil {
		return fleet.Host{}, false, err
	}
	host = host.Normalized()
	if err := host.Validate(); err != nil {
		return fleet.Host{}, false, err
	}
	if host.ID != hostID {
		return fleet.Host{}, false, errors.New("fleet host record id mismatch")
	}
	if host.IdentityPublic != strings.TrimSpace(currentIdentityPublic) {
		return fleet.Host{}, false, nil
	}
	host.IdentityPublic = strings.TrimSpace(newIdentityPublic)
	if err := host.Validate(); err != nil {
		return fleet.Host{}, false, err
	}
	updatedPayload, err := json.Marshal(host)
	if err != nil {
		return fleet.Host{}, false, err
	}
	updatedSealed, err := d.sealFleetHost(ctx, host.ID, updatedPayload)
	if err != nil {
		return fleet.Host{}, false, err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE fleet_hosts SET data_enc = ?, updated_at = ? WHERE id = ? AND state = ? AND data_enc = ?`,
		updatedSealed, time.Now().UTC().Unix(), host.ID, string(fleet.HostStateActive), sealed,
	)
	if err != nil {
		return fleet.Host{}, false, err
	}
	updated, err := result.RowsAffected()
	if err != nil || updated != 1 {
		return fleet.Host{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return fleet.Host{}, false, err
	}
	return host, true, nil
}

// fleet host record heartbeat atomically applies a strictly newer heartbeat to
// an active or stale host. a newer heartbeat restores a stale host to active.
// false means the host is missing, revoked, or stale input replayed an already
// recorded timestamp.
func (d *DB) FleetHostRecordHeartbeat(ctx context.Context, heartbeat fleet.Heartbeat) (fleet.Host, bool, error) {
	if err := heartbeat.Validate(); err != nil {
		return fleet.Host{}, false, err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fleet.Host{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var sealed []byte
	var state string
	var lastHeartbeatNS int64
	if err := tx.QueryRowContext(ctx, `SELECT data_enc, state, last_heartbeat_ns FROM fleet_hosts WHERE id = ?`, heartbeat.HostID).Scan(&sealed, &state, &lastHeartbeatNS); errors.Is(err, sql.ErrNoRows) {
		return fleet.Host{}, false, nil
	} else if err != nil {
		return fleet.Host{}, false, err
	}
	if (state != string(fleet.HostStateActive) && state != string(fleet.HostStateStale)) || heartbeat.SentAt.UTC().UnixNano() <= lastHeartbeatNS {
		return fleet.Host{}, false, nil
	}
	payload, err := d.openFleetHost(ctx, heartbeat.HostID, sealed)
	if err != nil {
		return fleet.Host{}, false, err
	}
	var host fleet.Host
	if err := json.Unmarshal(payload, &host); err != nil {
		return fleet.Host{}, false, err
	}
	host = host.Normalized()
	if err := host.Validate(); err != nil {
		return fleet.Host{}, false, err
	}
	if heartbeat.OwnerID != host.OwnerID {
		return fleet.Host{}, false, errors.New("fleet heartbeat owner mismatch")
	}
	host.LastSeenAt = heartbeat.SentAt.UTC()
	host.BinaryVersion = strings.TrimSpace(heartbeat.BinaryVersion)
	host.Capabilities = normalizedFleetCapabilities(heartbeat.Capabilities)
	previousBudget := host.Budget
	host.Budget = heartbeat.Budget.Normalized()
	if previousBudget.Date != "" && previousBudget.Date == host.Budget.Date && host.Budget.DailyTokens < previousBudget.DailyTokens {
		host.Budget.DailyTokens = previousBudget.DailyTokens
	}
	host.State = fleet.HostStateActive
	if err := host.Validate(); err != nil {
		return fleet.Host{}, false, err
	}
	payload, err = json.Marshal(host)
	if err != nil {
		return fleet.Host{}, false, err
	}
	updatedSealed, err := d.sealFleetHost(ctx, host.ID, payload)
	if err != nil {
		return fleet.Host{}, false, err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE fleet_hosts SET data_enc = ?, state = ?, last_seen_at = ?, last_heartbeat_ns = ?, updated_at = ?
		 WHERE id = ? AND state IN (?, ?) AND last_heartbeat_ns = ? AND data_enc = ?`,
		updatedSealed, string(fleet.HostStateActive), host.LastSeenAt.Unix(), host.LastSeenAt.UnixNano(), time.Now().UTC().Unix(), host.ID, string(fleet.HostStateActive), string(fleet.HostStateStale), lastHeartbeatNS, sealed,
	)
	if err != nil {
		return fleet.Host{}, false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return fleet.Host{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return fleet.Host{}, false, err
	}
	return host, true, nil
}

func (d *DB) FleetHostMarkStaleBefore(ctx context.Context, cutoff time.Time) (int, error) {
	if cutoff.IsZero() {
		return 0, errors.New("fleet host stale cutoff required")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx,
		`SELECT id, data_enc, last_seen_at, last_heartbeat_ns FROM fleet_hosts
		 WHERE state = ? AND (
		   (last_heartbeat_ns > 0 AND last_heartbeat_ns <= ?) OR
		   (last_heartbeat_ns = 0 AND last_seen_at > 0 AND last_seen_at <= ?)
		 )`,
		string(fleet.HostStateActive), cutoff.UTC().UnixNano(), cutoff.UTC().Unix())
	if err != nil {
		return 0, err
	}
	type candidate struct {
		id       string
		sealed   []byte
		lastSeen int64
		lastBeat int64
	}
	var candidates []candidate
	for rows.Next() {
		var candidate candidate
		if err := rows.Scan(&candidate.id, &candidate.sealed, &candidate.lastSeen, &candidate.lastBeat); err != nil {
			_ = rows.Close()
			return 0, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	marked := 0
	for _, candidate := range candidates {
		payload, err := d.openFleetHost(ctx, candidate.id, candidate.sealed)
		if err != nil {
			return 0, err
		}
		var host fleet.Host
		if err := json.Unmarshal(payload, &host); err != nil {
			return 0, err
		}
		host = host.Normalized()
		if err := host.Validate(); err != nil {
			return 0, err
		}
		host.State = fleet.HostStateStale
		if err := host.Validate(); err != nil {
			return 0, err
		}
		payload, err = json.Marshal(host)
		if err != nil {
			return 0, err
		}
		sealed, err := d.sealFleetHost(ctx, host.ID, payload)
		if err != nil {
			return 0, err
		}
		result, err := tx.ExecContext(ctx,
			`UPDATE fleet_hosts SET data_enc = ?, state = ?, updated_at = ?
			 WHERE id = ? AND state = ? AND last_seen_at = ? AND last_heartbeat_ns = ? AND data_enc = ?`,
			sealed, string(fleet.HostStateStale), time.Now().UTC().Unix(), host.ID, string(fleet.HostStateActive), candidate.lastSeen, candidate.lastBeat, candidate.sealed)
		if err != nil {
			return 0, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		marked += int(changed)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return marked, nil
}

func (d *DB) sealFleetHost(ctx context.Context, id string, payload []byte) ([]byte, error) {
	if d.cryptbox == nil {
		return nil, ErrCryptBoxUnavailable
	}
	return d.cryptbox.Seal(ctx, payload, RowAAD("fleet_hosts", id, "data_enc"))
}

func (d *DB) openFleetHost(ctx context.Context, id string, sealed []byte) ([]byte, error) {
	if d.cryptbox == nil {
		return nil, ErrCryptBoxUnavailable
	}
	return d.cryptbox.Open(ctx, sealed, RowAAD("fleet_hosts", id, "data_enc"))
}

type fleetHostScanner interface {
	Scan(dest ...any) error
}

func (d *DB) scanFleetHost(ctx context.Context, row fleetHostScanner) (fleet.Host, error) {
	var id string
	var sealed []byte
	if err := row.Scan(&id, &sealed); err != nil {
		return fleet.Host{}, err
	}
	payload, err := d.openFleetHost(ctx, id, sealed)
	if err != nil {
		return fleet.Host{}, err
	}
	var host fleet.Host
	if err := json.Unmarshal(payload, &host); err != nil {
		return fleet.Host{}, err
	}
	if host.ID != id {
		return fleet.Host{}, errors.New("fleet host record id mismatch")
	}
	return host.Normalized(), host.Validate()
}

func revokedAt(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return *v
}

func normalizedFleetCapabilities(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
