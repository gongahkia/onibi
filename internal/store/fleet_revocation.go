package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

const WebSessionReasonFleetEmergency = "fleet-emergency-revocation"

type FleetRevocationResult struct {
	Host               fleet.Host
	WebSessionsRevoked int
	ApprovalIDs        []string
}

func (d *DB) FleetHostEmergencyRevoke(ctx context.Context, ownerID, hostID string, now time.Time) (FleetRevocationResult, bool, error) {
	if strings.TrimSpace(ownerID) == "" || strings.TrimSpace(hostID) == "" || now.IsZero() {
		return FleetRevocationResult{}, false, errors.New("invalid fleet host revocation")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return FleetRevocationResult{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var sealed []byte
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT data_enc, state FROM fleet_hosts WHERE id = ?`, hostID).Scan(&sealed, &state); errors.Is(err, sql.ErrNoRows) {
		return FleetRevocationResult{}, false, nil
	} else if err != nil {
		return FleetRevocationResult{}, false, err
	}
	if state == string(fleet.HostStateRevoked) {
		return FleetRevocationResult{}, false, nil
	}
	payload, err := d.openFleetHost(ctx, hostID, sealed)
	if err != nil {
		return FleetRevocationResult{}, false, err
	}
	var host fleet.Host
	if err := json.Unmarshal(payload, &host); err != nil {
		return FleetRevocationResult{}, false, err
	}
	host = host.Normalized()
	if err := host.Validate(); err != nil || host.OwnerID != ownerID {
		return FleetRevocationResult{}, false, err
	}
	host.State = fleet.HostStateRevoked
	revokedAt := now.UTC()
	host.RevokedAt = &revokedAt
	if err := host.Validate(); err != nil {
		return FleetRevocationResult{}, false, err
	}
	payload, err = json.Marshal(host)
	if err != nil {
		return FleetRevocationResult{}, false, err
	}
	updatedSealed, err := d.sealFleetHost(ctx, host.ID, payload)
	if err != nil {
		return FleetRevocationResult{}, false, err
	}
	updated, err := tx.ExecContext(ctx, `UPDATE fleet_hosts SET data_enc = ?, state = ?, revoked_at = ?, updated_at = ? WHERE id = ? AND state != ? AND data_enc = ?`, updatedSealed, string(fleet.HostStateRevoked), revokedAt.Unix(), revokedAt.Unix(), host.ID, string(fleet.HostStateRevoked), sealed)
	if err != nil {
		return FleetRevocationResult{}, false, err
	}
	n, err := updated.RowsAffected()
	if err != nil || n != 1 {
		return FleetRevocationResult{}, false, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM approvals WHERE state = ?`, "pending")
	if err != nil {
		return FleetRevocationResult{}, false, err
	}
	var approvalIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return FleetRevocationResult{}, false, err
		}
		approvalIDs = append(approvalIDs, id)
	}
	if err := rows.Close(); err != nil {
		return FleetRevocationResult{}, false, err
	}
	if err := rows.Err(); err != nil {
		return FleetRevocationResult{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE approvals SET state = ?, reason = ?, decided_at = ? WHERE state = ?`, "cancelled", "fleet emergency host revocation", revokedAt.Unix(), "pending"); err != nil {
		return FleetRevocationResult{}, false, err
	}
	webSessions, err := tx.ExecContext(ctx, `UPDATE web_sessions SET revoked = 1, revoked_reason = ? WHERE revoked = 0`, WebSessionReasonFleetEmergency)
	if err != nil {
		return FleetRevocationResult{}, false, err
	}
	webSessionsRevoked, err := webSessions.RowsAffected()
	if err != nil {
		return FleetRevocationResult{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return FleetRevocationResult{}, false, err
	}
	return FleetRevocationResult{Host: host, WebSessionsRevoked: int(webSessionsRevoked), ApprovalIDs: approvalIDs}, true, nil
}
