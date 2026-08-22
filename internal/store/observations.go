package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
)

// RecordAvailabilityObservation stores only a slot-identity digest, never a
// partner response, session, or credential.
func (store *Store) RecordAvailabilityObservation(ctx context.Context, sourceID string, from, until time.Time, slots []domain.AvailabilitySlot, fetchedAt time.Time) error {
	identities := make([]string, 0, len(slots))
	for _, slot := range slots {
		identities = append(identities, slot.ID)
	}
	digest := sha256.Sum256([]byte(strings.Join(identities, "\x00")))
	reference := "availability:" + from.Format("2006-01-02") + ":" + until.Format("2006-01-02")
	id := sourceID + ":" + reference + ":" + fetchedAt.UTC().Format("20060102T150405.000000000")
	_, err := store.db.ExecContext(ctx, `INSERT INTO source_observations(id, source_id, reference, fetched_at, parser, evidence_hash, confidence)
VALUES (?, ?, ?, ?, ?, ?, ?)`, id, sourceID, reference, timestamp(fetchedAt), "partner-v1", hex.EncodeToString(digest[:]), 1.0)
	if err != nil {
		return fmt.Errorf("record source observation: %w", err)
	}
	return nil
}
