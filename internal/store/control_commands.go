package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

type ControlCommand struct {
	ID          string
	HostID      string
	SessionID   string
	Action      string
	Payload     []byte
	State       fleet.CommandState
	Result      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ExpiresAt   time.Time
	CompletedAt time.Time
}

func (c ControlCommand) Valid() bool {
	return strings.TrimSpace(c.ID) != "" && strings.TrimSpace(c.HostID) != "" && strings.TrimSpace(c.SessionID) != "" && strings.TrimSpace(c.Action) != "" && len(c.Action) <= 128 && c.State.Valid() && c.CreatedAt.Before(c.ExpiresAt) && !c.ExpiresAt.IsZero() && len(c.Result) <= 512
}

func (d *DB) ControlCommandCreate(ctx context.Context, command ControlCommand) (ControlCommand, bool, error) {
	if command.CreatedAt.IsZero() {
		command.CreatedAt = time.Now().UTC()
	}
	if command.UpdatedAt.IsZero() {
		command.UpdatedAt = command.CreatedAt
	}
	if !command.Valid() {
		return ControlCommand{}, false, errors.New("invalid control command")
	}
	payload, err := d.sealControlPayload(ctx, command.ID, command.Payload)
	if err != nil {
		return ControlCommand{}, false, err
	}
	res, err := d.sql.ExecContext(ctx,
		`INSERT INTO control_commands(id, host_id, session_id, action, payload_enc, state, result, created_at, updated_at, expires_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO NOTHING`,
		command.ID, command.HostID, command.SessionID, command.Action, payload, string(command.State), command.Result, command.CreatedAt.Unix(), command.UpdatedAt.Unix(), command.ExpiresAt.Unix(), unixOrZero(command.CompletedAt))
	if err != nil {
		return ControlCommand{}, false, err
	}
	created, err := res.RowsAffected()
	if err != nil {
		return ControlCommand{}, false, err
	}
	if created == 1 {
		return command, true, nil
	}
	existing, err := d.ControlCommand(ctx, command.ID)
	if err != nil {
		return ControlCommand{}, false, err
	}
	if existing.HostID != command.HostID || existing.SessionID != command.SessionID || existing.Action != command.Action || !bytes.Equal(existing.Payload, command.Payload) {
		return ControlCommand{}, false, errors.New("control command id collision")
	}
	return existing, false, nil
}

func (d *DB) ControlCommand(ctx context.Context, id string) (ControlCommand, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, host_id, session_id, action, payload_enc, state, result, created_at, updated_at, expires_at, completed_at
		 FROM control_commands WHERE id = ?`, id)
	return d.scanControlCommand(ctx, row)
}

func (d *DB) ControlCommandsActiveForHost(ctx context.Context, hostID string) ([]ControlCommand, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, host_id, session_id, action, payload_enc, state, result, created_at, updated_at, expires_at, completed_at
		 FROM control_commands WHERE host_id = ? AND state IN (?, ?) ORDER BY created_at ASC`, hostID, string(fleet.CommandPending), string(fleet.CommandDispatched))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ControlCommand
	for rows.Next() {
		command, err := d.scanControlCommand(ctx, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, command)
	}
	return out, rows.Err()
}

func (d *DB) ControlCommandMarkDispatched(ctx context.Context, id string, at time.Time) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE control_commands SET state = ?, updated_at = ? WHERE id = ? AND state = ?`, string(fleet.CommandDispatched), at.Unix(), id, string(fleet.CommandPending))
	return err
}

func (d *DB) ControlCommandComplete(ctx context.Context, id string, state fleet.CommandState, result string, at time.Time) (bool, error) {
	if !state.Terminal() || (state != fleet.CommandSucceeded && strings.TrimSpace(result) == "") || len(result) > 512 || at.IsZero() {
		return false, errors.New("invalid terminal control command")
	}
	res, err := d.sql.ExecContext(ctx,
		`UPDATE control_commands SET state = ?, result = ?, updated_at = ?, completed_at = ? WHERE id = ? AND state IN (?, ?)`, string(state), result, at.Unix(), at.Unix(), id, string(fleet.CommandPending), string(fleet.CommandDispatched))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (d *DB) ControlCommandsExpire(ctx context.Context, at time.Time) (int64, error) {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE control_commands SET state = ?, result = ?, updated_at = ?, completed_at = ? WHERE state IN (?, ?) AND expires_at <= ?`, string(fleet.CommandTimedOut), "command timed out", at.Unix(), at.Unix(), string(fleet.CommandPending), string(fleet.CommandDispatched), at.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type controlCommandScanner interface {
	Scan(...any) error
}

func (d *DB) scanControlCommand(ctx context.Context, scanner controlCommandScanner) (ControlCommand, error) {
	var command ControlCommand
	var payload []byte
	var state string
	var createdAt, updatedAt, expiresAt, completedAt int64
	if err := scanner.Scan(&command.ID, &command.HostID, &command.SessionID, &command.Action, &payload, &state, &command.Result, &createdAt, &updatedAt, &expiresAt, &completedAt); errors.Is(err, sql.ErrNoRows) {
		return ControlCommand{}, errors.New("unknown control command")
	} else if err != nil {
		return ControlCommand{}, err
	}
	var err error
	command.Payload, err = d.openControlPayload(ctx, command.ID, payload)
	if err != nil {
		return ControlCommand{}, err
	}
	command.State = fleet.CommandState(state)
	command.CreatedAt = time.Unix(createdAt, 0).UTC()
	command.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	command.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	if completedAt > 0 {
		command.CompletedAt = time.Unix(completedAt, 0).UTC()
	}
	if !command.Valid() || (command.State.Terminal() != !command.CompletedAt.IsZero()) {
		return ControlCommand{}, fmt.Errorf("invalid persisted control command %q", command.ID)
	}
	return command, nil
}

func (d *DB) sealControlPayload(ctx context.Context, id string, payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return []byte{}, nil
	}
	return d.sealBytes(ctx, "control_commands", id, "payload_enc", payload)
}

func (d *DB) openControlPayload(ctx context.Context, id string, payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	return d.openBytes(ctx, "control_commands", id, "payload_enc", payload)
}
