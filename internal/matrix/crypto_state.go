package matrix

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/gongahkia/onibi/internal/store"
)

const CryptoStateKVKey = "matrix.crypto.state.v1"

type CryptoState struct {
	UserID                 string                         `json:"user_id"`
	DeviceID               string                         `json:"device_id"`
	DeviceKeys             *DeviceKeys                    `json:"device_keys,omitempty"`
	AccountPickle          string                         `json:"account_pickle,omitempty"`
	AccountShared          bool                           `json:"account_shared,omitempty"`
	OneTimeKeyCounts       map[string]int                 `json:"one_time_key_counts,omitempty"`
	NextBatch              string                         `json:"next_batch,omitempty"`
	OlmSessions            map[string]OlmSessionState     `json:"olm_sessions,omitempty"`
	MegolmOutboundSessions map[string]MegolmOutboundState `json:"megolm_outbound_sessions,omitempty"`
	MegolmInboundSessions  map[string]MegolmInboundState  `json:"megolm_inbound_sessions,omitempty"`
	SASTransactions        map[string]SASTransactionState `json:"sas_transactions,omitempty"`
}

type OlmSessionState struct {
	UserID     string `json:"user_id"`
	DeviceID   string `json:"device_id"`
	SenderKey  string `json:"sender_key"`
	SessionID  string `json:"session_id"`
	Pickle     string `json:"pickle"`
	LastUsedAt string `json:"last_used_at,omitempty"`
}

type MegolmOutboundState struct {
	RoomID       string              `json:"room_id"`
	SessionID    string              `json:"session_id"`
	Pickle       string              `json:"pickle"`
	MessageIndex int                 `json:"message_index,omitempty"`
	SharedWith   map[string][]string `json:"shared_with,omitempty"`
}

type MegolmInboundState struct {
	RoomID          string `json:"room_id"`
	SenderKey       string `json:"sender_key"`
	SessionID       string `json:"session_id"`
	Pickle          string `json:"pickle"`
	FirstKnownIndex int    `json:"first_known_index,omitempty"`
}

type SASTransactionState struct {
	TransactionID      string `json:"transaction_id"`
	UserID             string `json:"user_id"`
	DeviceID           string `json:"device_id"`
	FlowID             string `json:"flow_id,omitempty"`
	State              string `json:"state"`
	EphemeralPublicKey string `json:"ephemeral_public_key,omitempty"`
	Commitment         string `json:"commitment,omitempty"`
	StartedAt          string `json:"started_at,omitempty"`
}

func SaveCryptoState(ctx context.Context, db *store.DB, state CryptoState) error {
	if db == nil {
		return errors.New("matrix crypto state db required")
	}
	state.UserID = strings.TrimSpace(state.UserID)
	state.DeviceID = strings.TrimSpace(state.DeviceID)
	if state.UserID == "" || state.DeviceID == "" {
		return errors.New("matrix crypto state requires user and device id")
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return db.KVSetEncryptedString(ctx, CryptoStateKVKey, string(b))
}

func LoadCryptoState(ctx context.Context, db *store.DB) (CryptoState, bool, error) {
	if db == nil {
		return CryptoState{}, false, errors.New("matrix crypto state db required")
	}
	raw, ok, err := db.KVGetEncryptedString(ctx, CryptoStateKVKey)
	if err != nil || !ok {
		return CryptoState{}, ok, err
	}
	var state CryptoState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return CryptoState{}, true, err
	}
	return state, true, nil
}
