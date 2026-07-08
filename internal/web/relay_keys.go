package web

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/store"
)

const (
	relayPairCommitPrefix    = "relay_key_commitment:pair:"
	relaySessionCommitPrefix = "relay_key_commitment:session:"
	relayPairVerifierInfo    = "onibi-e2e-pair-verifier-v1"
	relayVerifyTokenInfo     = "onibi-e2e-session-verifier-v1"
)

type RelayKeys struct {
	mu       sync.Mutex
	pairs    map[string]relayPairKey
	sessions map[string][]byte
}

type relayPairKey struct {
	key     []byte
	expires time.Time
}

func NewRelayKeys() *RelayKeys {
	return &RelayKeys{pairs: map[string]relayPairKey{}, sessions: map[string][]byte{}}
}

func (r *RelayKeys) RegisterPair(ctx context.Context, db *store.DB, token string, key []byte, ttl time.Duration) error {
	if r == nil {
		return errors.New("relay key store unavailable")
	}
	if _, err := envelope.NewCodec(key, "commit-check"); err != nil {
		return err
	}
	expire := time.Now().Add(ttl)
	r.mu.Lock()
	r.pairs[token] = relayPairKey{key: append([]byte(nil), key...), expires: expire}
	r.mu.Unlock()
	if db == nil {
		return nil
	}
	return db.KVSetString(ctx, relayPairCommitPrefix+token, envelope.Commitment(key))
}

func (r *RelayKeys) BindSession(ctx context.Context, db *store.DB, token, sessionID string) (bool, error) {
	if r == nil {
		return false, nil
	}
	now := time.Now()
	r.mu.Lock()
	pair, ok := r.pairs[token]
	if ok {
		delete(r.pairs, token)
	}
	if !ok || now.After(pair.expires) {
		r.mu.Unlock()
		return false, nil
	}
	r.sessions[sessionID] = append([]byte(nil), pair.key...)
	r.mu.Unlock()
	if db == nil {
		return true, nil
	}
	verifyToken, err := relayVerifyToken(pair.key, sessionID)
	if err != nil {
		return true, err
	}
	if ok, err := db.SetWebSessionKeyVerifier(ctx, sessionID, verifyToken); err != nil {
		return true, err
	} else if !ok {
		return true, errors.New("relay session verifier target missing")
	}
	if err := db.KVSetString(ctx, relaySessionCommitPrefix+sessionID, envelope.Commitment(pair.key)); err != nil {
		return true, err
	}
	return true, nil
}

func (r *RelayKeys) KeyForPair(token string) ([]byte, bool) {
	if r == nil {
		return nil, false
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	pair, ok := r.pairs[token]
	if !ok {
		return nil, false
	}
	if now.After(pair.expires) {
		delete(r.pairs, token)
		return nil, false
	}
	return append([]byte(nil), pair.key...), true
}

func (r *RelayKeys) KeyForSession(sessionID string) ([]byte, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.sessions[sessionID]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), key...), true
}

func relayPairVerifier(key []byte, token string) ([]byte, error) {
	if len(key) != envelope.KeyBytes {
		return nil, errors.New("relay key must be 32 bytes")
	}
	if token == "" {
		return nil, errors.New("relay pair token required")
	}
	return hkdf.Key(sha256.New, key, []byte(token), relayPairVerifierInfo, envelope.KeyBytes)
}

func relayVerifyToken(key []byte, sessionID string) ([]byte, error) {
	if len(key) != envelope.KeyBytes {
		return nil, errors.New("relay key must be 32 bytes")
	}
	if sessionID == "" {
		return nil, errors.New("relay session id required")
	}
	return hkdf.Key(sha256.New, key, []byte(sessionID), relayVerifyTokenInfo, envelope.KeyBytes)
}
