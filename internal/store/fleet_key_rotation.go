package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

func (d *DB) FleetKeyRotationIssue(ctx context.Context, challenge fleet.KeyRotationChallenge) error {
	if err := challenge.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(challenge)
	if err != nil {
		return err
	}
	sealed, err := d.sealFleetKeyRotation(ctx, challenge.ID, payload)
	if err != nil {
		return err
	}
	nonceHash := sha256.Sum256([]byte(challenge.Nonce))
	_, err = d.sql.ExecContext(ctx,
		`INSERT INTO fleet_key_rotation_challenges(id, challenge_enc, nonce_hash, expires_at, consumed_at, created_at)
		 VALUES (?, ?, ?, ?, 0, ?)`,
		challenge.ID, sealed, nonceHash[:], challenge.ExpiresAt.UTC().Unix(), time.Now().UTC().Unix(),
	)
	return err
}

func (d *DB) FleetKeyRotationGet(ctx context.Context, challengeID string) (fleet.KeyRotationChallenge, bool, error) {
	var sealed []byte
	var expiresAt, consumedAt int64
	err := d.sql.QueryRowContext(ctx,
		`SELECT challenge_enc, expires_at, consumed_at FROM fleet_key_rotation_challenges WHERE id = ?`, challengeID,
	).Scan(&sealed, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return fleet.KeyRotationChallenge{}, false, nil
	}
	if err != nil {
		return fleet.KeyRotationChallenge{}, false, err
	}
	if consumedAt != 0 || expiresAt <= time.Now().UTC().Unix() {
		return fleet.KeyRotationChallenge{}, false, nil
	}
	payload, err := d.openFleetKeyRotation(ctx, challengeID, sealed)
	if err != nil {
		return fleet.KeyRotationChallenge{}, false, err
	}
	var challenge fleet.KeyRotationChallenge
	if err := json.Unmarshal(payload, &challenge); err != nil {
		return fleet.KeyRotationChallenge{}, false, err
	}
	if err := challenge.Validate(); err != nil {
		return fleet.KeyRotationChallenge{}, false, err
	}
	return challenge, true, nil
}

func (d *DB) FleetKeyRotationConsume(ctx context.Context, challengeID, nonce string) (bool, error) {
	nonceHash := sha256.Sum256([]byte(nonce))
	result, err := d.sql.ExecContext(ctx,
		`UPDATE fleet_key_rotation_challenges SET consumed_at = ?
		 WHERE id = ? AND nonce_hash = ? AND expires_at > ? AND consumed_at = 0`,
		time.Now().UTC().Unix(), challengeID, nonceHash[:], time.Now().UTC().Unix(),
	)
	if err != nil {
		return false, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return updated == 1, nil
}

func (d *DB) sealFleetKeyRotation(ctx context.Context, id string, payload []byte) ([]byte, error) {
	if d.cryptbox == nil {
		return nil, ErrCryptBoxUnavailable
	}
	return d.cryptbox.Seal(ctx, payload, RowAAD("fleet_key_rotation_challenges", id, "challenge_enc"))
}

func (d *DB) openFleetKeyRotation(ctx context.Context, id string, sealed []byte) ([]byte, error) {
	if d.cryptbox == nil {
		return nil, ErrCryptBoxUnavailable
	}
	return d.cryptbox.Open(ctx, sealed, RowAAD("fleet_key_rotation_challenges", id, "challenge_enc"))
}
