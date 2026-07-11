package matrix

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
	PickleKey              string                         `json:"pickle_key,omitempty"`
	AccountPickle          string                         `json:"account_pickle,omitempty"`
	AccountShared          bool                           `json:"account_shared,omitempty"`
	OneTimeKeyCounts       map[string]int                 `json:"one_time_key_counts,omitempty"`
	NextBatch              string                         `json:"next_batch,omitempty"`
	OlmSessions            map[string]OlmSessionState     `json:"olm_sessions,omitempty"`
	MegolmOutboundSessions map[string]MegolmOutboundState `json:"megolm_outbound_sessions,omitempty"`
	MegolmInboundSessions  map[string]MegolmInboundState  `json:"megolm_inbound_sessions,omitempty"`
	SASTransactions        map[string]SASTransactionState `json:"sas_transactions,omitempty"`
	TrustedDevices         map[string][]string            `json:"trusted_devices,omitempty"`
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

func EnsureCryptoState(ctx context.Context, db *store.DB, userID, deviceID string, oneTimeKeyCount uint) (CryptoState, []byte, bool, error) {
	state, ok, err := LoadCryptoState(ctx, db)
	if err != nil {
		return CryptoState{}, nil, false, err
	}
	if ok {
		userID = strings.TrimSpace(userID)
		deviceID = strings.TrimSpace(deviceID)
		if userID != "" && state.UserID != userID {
			return CryptoState{}, nil, false, errors.New("matrix crypto state user mismatch")
		}
		if deviceID != "" && state.DeviceID != deviceID {
			return CryptoState{}, nil, false, errors.New("matrix crypto state device mismatch")
		}
		if strings.TrimSpace(state.PickleKey) == "" && strings.TrimSpace(state.AccountPickle) == "" {
			return initializeCryptoState(ctx, db, state.UserID, state.DeviceID, oneTimeKeyCount)
		}
		pickleKey, err := state.PickleKeyBytes()
		if err != nil {
			return CryptoState{}, nil, false, err
		}
		return state, pickleKey, false, nil
	}
	return initializeCryptoState(ctx, db, userID, deviceID, oneTimeKeyCount)
}

func initializeCryptoState(ctx context.Context, db *store.DB, userID, deviceID string, oneTimeKeyCount uint) (CryptoState, []byte, bool, error) {
	pickleKey := make([]byte, 32)
	if _, err := rand.Read(pickleKey); err != nil {
		return CryptoState{}, nil, false, err
	}
	state, err := NewOlmAccountState(userID, deviceID, pickleKey, oneTimeKeyCount)
	if err != nil {
		return CryptoState{}, nil, false, err
	}
	state.PickleKey = base64.RawStdEncoding.EncodeToString(pickleKey)
	if err := SaveCryptoState(ctx, db, state); err != nil {
		return CryptoState{}, nil, false, err
	}
	return state, pickleKey, true, nil
}

func (s CryptoState) PickleKeyBytes() ([]byte, error) {
	if strings.TrimSpace(s.PickleKey) == "" {
		return nil, errors.New("matrix crypto state pickle key required")
	}
	key, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(s.PickleKey))
	if err != nil {
		return nil, err
	}
	if len(key) == 0 {
		return nil, errors.New("matrix crypto state pickle key required")
	}
	return key, nil
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
