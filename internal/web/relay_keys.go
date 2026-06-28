package web

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/store"
)

const (
	relayPairCommitPrefix    = "relay_key_commitment:pair:"
	relaySessionCommitPrefix = "relay_key_commitment:session:"
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
	if err := db.KVSetString(ctx, relaySessionCommitPrefix+sessionID, envelope.Commitment(pair.key)); err != nil {
		return true, err
	}
	return true, nil
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
