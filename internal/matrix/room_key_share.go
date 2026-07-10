package matrix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type RoomKeyShareTarget struct {
	UserID   string
	DeviceID string
}

type OlmPayload struct {
	Type             string            `json:"type"`
	Content          any               `json:"content"`
	Sender           string            `json:"sender"`
	Recipient        string            `json:"recipient"`
	Keys             map[string]string `json:"keys"`
	RecipientKeys    map[string]string `json:"recipient_keys"`
	SenderDeviceKeys *DeviceKeys       `json:"sender_device_keys,omitempty"`
}

func (c *Client) ShareRoomKeyWithDevices(ctx context.Context, state CryptoState, outbound MegolmOutboundState, roomKey RoomKeyContent, pickleKey []byte, targets []RoomKeyShareTarget, timeout time.Duration) (CryptoState, MegolmOutboundState, error) {
	if c == nil {
		return state, outbound, errors.New("matrix client nil")
	}
	if len(pickleKey) == 0 {
		return state, outbound, errors.New("matrix room key share pickle key required")
	}
	if state.DeviceKeys == nil {
		return state, outbound, errors.New("matrix room key share sender device keys required")
	}
	if strings.TrimSpace(roomKey.Algorithm) != AlgorithmMegolmV1 || strings.TrimSpace(roomKey.RoomID) == "" || strings.TrimSpace(roomKey.SessionID) == "" || strings.TrimSpace(roomKey.SessionKey) == "" {
		return state, outbound, errors.New("matrix room key share room key incomplete")
	}
	if outbound.SessionID != "" && outbound.SessionID != roomKey.SessionID {
		return state, outbound, errors.New("matrix room key share session mismatch")
	}
	targets, err := normalizeRoomKeyShareTargets(targets)
	if err != nil {
		return state, outbound, err
	}
	senderEd25519 := deviceKeyValue(state.DeviceKeys, "ed25519")
	if senderEd25519 == "" {
		return state, outbound, errors.New("matrix room key share sender ed25519 key required")
	}
	queryReq := roomKeyShareDeviceMap(targets)
	query, err := c.QueryKeys(ctx, queryReq, timeout)
	if err != nil {
		return state, outbound, err
	}
	queried := map[string]DeviceKeys{}
	for _, target := range targets {
		deviceKeys, err := roomKeyShareDeviceKeys(query, target)
		if err != nil {
			return state, outbound, err
		}
		if deviceKeyValue(&deviceKeys, "curve25519") == "" || deviceKeyValue(&deviceKeys, "ed25519") == "" {
			return state, outbound, fmt.Errorf("matrix room key share device keys incomplete for %s/%s", target.UserID, target.DeviceID)
		}
		queried[roomKeyShareTargetKey(target)] = deviceKeys
	}
	claimReq := map[string]map[string]string{}
	for userID, deviceIDs := range queryReq {
		claimReq[userID] = map[string]string{}
		for _, deviceID := range deviceIDs {
			claimReq[userID][deviceID] = KeyAlgorithmSignedCurve255
		}
	}
	claim, err := c.ClaimOneTimeKeys(ctx, claimReq, timeout)
	if err != nil {
		return state, outbound, err
	}
	claimed := map[string]string{}
	for _, target := range targets {
		oneTimeKey, err := roomKeyShareOneTimeKey(claim, target)
		if err != nil {
			return state, outbound, err
		}
		claimed[roomKeyShareTargetKey(target)] = oneTimeKey
	}
	nextState := state
	if nextState.OlmSessions == nil {
		nextState.OlmSessions = map[string]OlmSessionState{}
	}
	nextOutbound := outbound
	if nextOutbound.SharedWith == nil {
		nextOutbound.SharedWith = map[string][]string{}
	}
	messages := map[string]map[string]any{}
	for _, target := range targets {
		deviceKeys := queried[roomKeyShareTargetKey(target)]
		recipientCurve25519 := deviceKeyValue(&deviceKeys, "curve25519")
		recipientEd25519 := deviceKeyValue(&deviceKeys, "ed25519")
		oneTimeKey := claimed[roomKeyShareTargetKey(target)]
		payload := OlmPayload{
			Type:             EventRoomKey,
			Content:          roomKey,
			Sender:           state.UserID,
			Recipient:        target.UserID,
			Keys:             map[string]string{"ed25519": senderEd25519},
			RecipientKeys:    map[string]string{"ed25519": recipientEd25519},
			SenderDeviceKeys: state.DeviceKeys,
		}
		plaintext, err := json.Marshal(payload)
		if err != nil {
			return state, outbound, err
		}
		var session OlmSessionState
		var content OlmEncryptedContent
		nextState, session, content, err = EncryptOlmForDevice(nextState, pickleKey, target.UserID, target.DeviceID, recipientCurve25519, oneTimeKey, plaintext)
		if err != nil {
			return state, outbound, err
		}
		nextState.OlmSessions[OlmSessionKey(target.UserID, target.DeviceID, session.SessionID)] = session
		if messages[target.UserID] == nil {
			messages[target.UserID] = map[string]any{}
		}
		messages[target.UserID][target.DeviceID] = content
		nextOutbound.SharedWith[target.UserID] = appendUniqueString(nextOutbound.SharedWith[target.UserID], target.DeviceID)
	}
	if err := c.SendToDevice(ctx, EventRoomEncrypted, messages); err != nil {
		return state, outbound, err
	}
	return nextState, nextOutbound, nil
}

func OlmSessionKey(userID, deviceID, sessionID string) string {
	parts := []string{strings.TrimSpace(userID), strings.TrimSpace(deviceID), strings.TrimSpace(sessionID)}
	return strings.Join(parts, "/")
}

func normalizeRoomKeyShareTargets(targets []RoomKeyShareTarget) ([]RoomKeyShareTarget, error) {
	if len(targets) == 0 {
		return nil, errors.New("matrix room key share targets required")
	}
	seen := map[string]bool{}
	out := make([]RoomKeyShareTarget, 0, len(targets))
	for _, target := range targets {
		target.UserID = strings.TrimSpace(target.UserID)
		target.DeviceID = strings.TrimSpace(target.DeviceID)
		if target.UserID == "" || target.DeviceID == "" {
			return nil, errors.New("matrix room key share target user and device required")
		}
		key := target.UserID + "\x00" + target.DeviceID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, target)
	}
	return out, nil
}

func roomKeyShareDeviceMap(targets []RoomKeyShareTarget) map[string][]string {
	out := map[string][]string{}
	for _, target := range targets {
		out[target.UserID] = append(out[target.UserID], target.DeviceID)
	}
	for userID := range out {
		sort.Strings(out[userID])
	}
	return out
}

func roomKeyShareDeviceKeys(resp KeysQueryResponse, target RoomKeyShareTarget) (DeviceKeys, error) {
	byUser := resp.DeviceKeys[target.UserID]
	if len(byUser) == 0 {
		return DeviceKeys{}, fmt.Errorf("matrix room key share no device keys for %s", target.UserID)
	}
	raw := byUser[target.DeviceID]
	if len(raw) == 0 {
		return DeviceKeys{}, fmt.Errorf("matrix room key share no device key for %s/%s", target.UserID, target.DeviceID)
	}
	var keys DeviceKeys
	if err := json.Unmarshal(raw, &keys); err != nil {
		return DeviceKeys{}, err
	}
	if keys.UserID != target.UserID || keys.DeviceID != target.DeviceID || !roomKeyShareContainsString(keys.Algorithms, AlgorithmOlmV1) || !roomKeyShareContainsString(keys.Algorithms, AlgorithmMegolmV1) {
		return DeviceKeys{}, fmt.Errorf("matrix room key share unsupported device %s/%s", target.UserID, target.DeviceID)
	}
	return keys, nil
}

func roomKeyShareOneTimeKey(resp KeysClaimResponse, target RoomKeyShareTarget) (string, error) {
	byUser := resp.OneTimeKeys[target.UserID]
	if len(byUser) == 0 {
		return "", fmt.Errorf("matrix room key share no one-time keys for %s", target.UserID)
	}
	byDevice := byUser[target.DeviceID]
	if len(byDevice) == 0 {
		return "", fmt.Errorf("matrix room key share no one-time key for %s/%s", target.UserID, target.DeviceID)
	}
	keyIDs := make([]string, 0, len(byDevice))
	for keyID := range byDevice {
		keyIDs = append(keyIDs, keyID)
	}
	sort.Strings(keyIDs)
	for _, keyID := range keyIDs {
		if !strings.HasPrefix(keyID, KeyAlgorithmSignedCurve255+":") {
			continue
		}
		key, err := oneTimeKeyValue(byDevice[keyID])
		if err != nil {
			return "", err
		}
		if key != "" {
			return key, nil
		}
	}
	return "", fmt.Errorf("matrix room key share signed one-time key missing for %s/%s", target.UserID, target.DeviceID)
}

func oneTimeKeyValue(raw json.RawMessage) (string, error) {
	var obj struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && strings.TrimSpace(obj.Key) != "" {
		return strings.TrimSpace(obj.Key), nil
	}
	var key string
	if err := json.Unmarshal(raw, &key); err != nil {
		return "", err
	}
	return strings.TrimSpace(key), nil
}

func deviceKeyValue(keys *DeviceKeys, algorithm string) string {
	if keys == nil {
		return ""
	}
	return strings.TrimSpace(keys.Keys[algorithm+":"+keys.DeviceID])
}

func roomKeyShareTargetKey(target RoomKeyShareTarget) string {
	return target.UserID + "\x00" + target.DeviceID
}

func roomKeyShareContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
