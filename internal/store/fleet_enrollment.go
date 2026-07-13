package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

const fleetOwnerKey = "fleet_owner_id"

func (d *DB) FleetOwnerID(ctx context.Context) (string, error) {
	if ownerID, ok, err := d.KVGetEncryptedString(ctx, fleetOwnerKey); err != nil || ok {
		return ownerID, err
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	ownerID := "owner-" + hex.EncodeToString(buf)
	inserted, err := d.KVSetEncryptedStringIfAbsent(ctx, fleetOwnerKey, ownerID)
	if err != nil {
		return "", err
	}
	if inserted {
		return ownerID, nil
	}
	ownerID, ok, err := d.KVGetEncryptedString(ctx, fleetOwnerKey)
	if err != nil || !ok {
		if err != nil {
			return "", fmt.Errorf("read persisted fleet owner id: %w", err)
		}
		return "", errors.New("persisted fleet owner id missing")
	}
	return ownerID, nil
}

type FleetEnrollment struct {
	Challenge fleet.EnrollmentChallenge
	Host      fleet.Host
}

func (d *DB) FleetEnrollmentIssue(ctx context.Context, challenge fleet.EnrollmentChallenge, host fleet.Host) error {
	if err := challenge.Validate(); err != nil {
		return err
	}
	host = host.Normalized()
	if err := host.Validate(); err != nil {
		return err
	}
	if host.State != fleet.HostStatePending {
		return errors.New("fleet enrollment host must be pending")
	}
	payload, err := json.Marshal(host)
	if err != nil {
		return err
	}
	sealed, err := d.sealFleetEnrollment(ctx, challenge.ID, payload)
	if err != nil {
		return err
	}
	nonceHash := sha256.Sum256([]byte(challenge.Nonce))
	_, err = d.sql.ExecContext(ctx,
		`INSERT INTO fleet_enrollment_challenges(id, host_enc, nonce_hash, expires_at, consumed_at, created_at)
		 VALUES (?, ?, ?, ?, 0, ?)`,
		challenge.ID, sealed, nonceHash[:], challenge.ExpiresAt.UTC().Unix(), time.Now().UTC().Unix())
	return err
}

func (d *DB) FleetEnrollmentGet(ctx context.Context, challengeID string) (FleetEnrollment, bool, error) {
	var sealed []byte
	var expiresAt, consumedAt int64
	err := d.sql.QueryRowContext(ctx,
		`SELECT host_enc, expires_at, consumed_at FROM fleet_enrollment_challenges WHERE id = ?`, challengeID,
	).Scan(&sealed, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return FleetEnrollment{}, false, nil
	}
	if err != nil {
		return FleetEnrollment{}, false, err
	}
	if consumedAt != 0 || expiresAt <= time.Now().UTC().Unix() {
		return FleetEnrollment{}, false, nil
	}
	host, err := d.openFleetEnrollmentHost(ctx, challengeID, sealed)
	if err != nil {
		return FleetEnrollment{}, false, err
	}
	return FleetEnrollment{Challenge: fleet.EnrollmentChallenge{Version: fleet.ProtocolVersion, ID: challengeID, OwnerID: host.OwnerID, ExpiresAt: time.Unix(expiresAt, 0).UTC()}, Host: host}, true, nil
}

func (d *DB) FleetEnrollmentConsume(ctx context.Context, challengeID, nonce string) (bool, error) {
	nonceHash := sha256.Sum256([]byte(nonce))
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var storedHash []byte
	var expiresAt, consumedAt int64
	err = tx.QueryRowContext(ctx,
		`SELECT nonce_hash, expires_at, consumed_at FROM fleet_enrollment_challenges WHERE id = ?`, challengeID,
	).Scan(&storedHash, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) || consumedAt != 0 || expiresAt <= time.Now().UTC().Unix() {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(storedHash) != len(nonceHash) || subtle.ConstantTimeCompare(storedHash, nonceHash[:]) != 1 {
		return false, nil
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE fleet_enrollment_challenges SET consumed_at = ? WHERE id = ? AND consumed_at = 0`, time.Now().UTC().Unix(), challengeID,
	)
	if err != nil {
		return false, err
	}
	updated, err := result.RowsAffected()
	if err != nil || updated != 1 {
		return false, err
	}
	return true, tx.Commit()
}

func (d *DB) FleetEnrollmentPurgeExpired(ctx context.Context, now time.Time) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM fleet_enrollment_challenges WHERE expires_at <= ? OR consumed_at != 0`, now.UTC().Unix())
	return err
}

func (d *DB) sealFleetEnrollment(ctx context.Context, id string, payload []byte) ([]byte, error) {
	if d.cryptbox == nil {
		return nil, ErrCryptBoxUnavailable
	}
	return d.cryptbox.Seal(ctx, payload, RowAAD("fleet_enrollment_challenges", id, "host_enc"))
}

func (d *DB) openFleetEnrollmentHost(ctx context.Context, id string, sealed []byte) (fleet.Host, error) {
	if d.cryptbox == nil {
		return fleet.Host{}, ErrCryptBoxUnavailable
	}
	payload, err := d.cryptbox.Open(ctx, sealed, RowAAD("fleet_enrollment_challenges", id, "host_enc"))
	if err != nil {
		return fleet.Host{}, err
	}
	var host fleet.Host
	if err := json.Unmarshal(payload, &host); err != nil {
		return fleet.Host{}, err
	}
	host = host.Normalized()
	if err := host.Validate(); err != nil {
		return fleet.Host{}, err
	}
	return host, nil
}
