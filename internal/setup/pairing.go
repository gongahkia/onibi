package setup

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

// PairTokenTTL is how long a pairing token remains usable.
const PairTokenTTL = 5 * time.Minute

// pairPayloadBytes is the random payload length before base64url. 32 bytes
// gives 43-char URL-safe payload; well within Telegram's 64-char start
// parameter limit.
const pairPayloadBytes = 32

// PairPrefix is the start-parameter prefix Telegram delivers to the bot
// when the user taps the deeplink. Full param looks like "pair_<43chars>".
const PairPrefix = "pair_"

// ErrPairExpired and ErrPairConsumed surface specific failure modes for the
// caller to render distinct messages. The bot must never disclose which
// error occurred to non-owners; for the user themselves it's helpful.
var (
	ErrPairExpired  = errors.New("pairing token expired or unknown")
	ErrPairConsumed = ErrPairExpired // single-use violation looks identical externally
)

// NewToken generates and persists a fresh single-use pairing token.
// Returns the bare token (no "pair_" prefix) so the wizard can format the
// deeplink + QR however it wants.
func NewToken(ctx context.Context, db *store.DB) (string, error) {
	raw := make([]byte, pairPayloadBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("pair token rand: %w", err)
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	if err := db.PutPairingToken(ctx, tok, PairTokenTTL); err != nil {
		return "", fmt.Errorf("persist pair token: %w", err)
	}
	return tok, nil
}

// DeepLink returns the t.me URL for the bot username + raw token.
// botUsername must be without leading @.
func DeepLink(botUsername, token string) string {
	return fmt.Sprintf("https://t.me/%s?start=%s%s", botUsername, PairPrefix, token)
}

// ExtractToken parses a /start payload of the form "pair_<token>" and
// returns the bare token. Returns false if the payload is malformed.
func ExtractToken(startPayload string) (string, bool) {
	if !strings.HasPrefix(startPayload, PairPrefix) {
		return "", false
	}
	return strings.TrimPrefix(startPayload, PairPrefix), true
}

// Consume atomically marks the token consumed. Returns nil only on the
// single winning consume call; ErrPairExpired otherwise (covers expiry,
// double-spend, and unknown-token in one rejection path so we don't leak
// which case it was).
func Consume(ctx context.Context, db *store.DB, token string) error {
	ok, err := db.ConsumePairingToken(ctx, token)
	if err != nil {
		return err
	}
	if !ok {
		return ErrPairExpired
	}
	return nil
}
