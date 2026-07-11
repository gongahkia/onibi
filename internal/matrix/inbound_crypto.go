package matrix

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func DeviceKeysFromQuery(resp KeysQueryResponse, userID, deviceID string) (DeviceKeys, error) {
	userID = strings.TrimSpace(userID)
	deviceID = strings.TrimSpace(deviceID)
	if userID == "" || deviceID == "" {
		return DeviceKeys{}, errors.New("matrix device user and device id required")
	}
	raw := resp.DeviceKeys[userID][deviceID]
	if len(raw) == 0 {
		return DeviceKeys{}, fmt.Errorf("matrix device keys missing for %s/%s", userID, deviceID)
	}
	var keys DeviceKeys
	if err := json.Unmarshal(raw, &keys); err != nil {
		return DeviceKeys{}, err
	}
	if keys.UserID != userID || keys.DeviceID != deviceID || deviceKeyValue(&keys, "curve25519") == "" || deviceKeyValue(&keys, "ed25519") == "" || !roomKeyShareContainsString(keys.Algorithms, AlgorithmOlmV1) || !roomKeyShareContainsString(keys.Algorithms, AlgorithmMegolmV1) {
		return DeviceKeys{}, fmt.Errorf("matrix device keys incomplete for %s/%s", userID, deviceID)
	}
	return keys, nil
}

func DecryptOlmToDevice(state CryptoState, pickleKey []byte, content OlmEncryptedContent) (CryptoState, OlmSessionState, OlmPayload, error) {
	if strings.TrimSpace(content.Algorithm) != AlgorithmOlmV1 || strings.TrimSpace(content.SenderKey) == "" {
		return state, OlmSessionState{}, OlmPayload{}, errors.New("matrix olm to-device event incomplete")
	}
	ownCurve25519 := ownCurve25519(state)
	if ownCurve25519 == "" {
		return state, OlmSessionState{}, OlmPayload{}, errors.New("matrix olm local curve25519 key required")
	}
	for key, session := range state.OlmSessions {
		if session.SenderKey != content.SenderKey {
			continue
		}
		nextSession, plaintext, err := DecryptOlmWithSession(session, pickleKey, content, ownCurve25519)
		if err != nil {
			continue
		}
		payload, err := decodeOlmPayload(plaintext)
		if err != nil {
			return state, OlmSessionState{}, OlmPayload{}, err
		}
		state.OlmSessions[key] = nextSession
		return state, nextSession, payload, nil
	}
	if info, ok := content.Ciphertext[ownCurve25519]; !ok || info.Type != OlmMessageTypePreKey {
		return state, OlmSessionState{}, OlmPayload{}, errors.New("matrix olm session missing for to-device event")
	}
	nextState, session, plaintext, err := DecryptOlmFromDevice(state, pickleKey, content)
	if err != nil {
		return state, OlmSessionState{}, OlmPayload{}, err
	}
	payload, err := decodeOlmPayload(plaintext)
	if err != nil {
		return state, OlmSessionState{}, OlmPayload{}, err
	}
	return nextState, session, payload, nil
}

func ValidateOlmRoomKeyPayload(payload OlmPayload, sender string, state CryptoState, trusted TrustedDeviceKey) (RoomKeyContent, error) {
	sender = strings.TrimSpace(sender)
	if payload.Type != EventRoomKey || strings.TrimSpace(payload.Sender) != sender || strings.TrimSpace(payload.Recipient) != state.UserID {
		return RoomKeyContent{}, errors.New("matrix olm room key payload identity mismatch")
	}
	if payload.Keys["ed25519"] != trusted.Ed25519Key || payload.RecipientKeys["ed25519"] != deviceKeyValue(state.DeviceKeys, "ed25519") {
		return RoomKeyContent{}, errors.New("matrix olm room key payload signing key mismatch")
	}
	raw, err := json.Marshal(payload.Content)
	if err != nil {
		return RoomKeyContent{}, err
	}
	var roomKey RoomKeyContent
	if err := json.Unmarshal(raw, &roomKey); err != nil {
		return RoomKeyContent{}, err
	}
	if roomKey.Algorithm != AlgorithmMegolmV1 || strings.TrimSpace(roomKey.RoomID) == "" || strings.TrimSpace(roomKey.SessionID) == "" || strings.TrimSpace(roomKey.SessionKey) == "" {
		return RoomKeyContent{}, errors.New("matrix olm room key payload incomplete")
	}
	return roomKey, nil
}

func StoreMegolmInboundSession(state CryptoState, incoming MegolmInboundState) (CryptoState, error) {
	if strings.TrimSpace(incoming.RoomID) == "" || strings.TrimSpace(incoming.SenderKey) == "" || strings.TrimSpace(incoming.SessionID) == "" || strings.TrimSpace(incoming.Pickle) == "" {
		return state, errors.New("matrix megolm inbound session incomplete")
	}
	if state.MegolmInboundSessions == nil {
		state.MegolmInboundSessions = map[string]MegolmInboundState{}
	}
	key := MegolmInboundSessionKey(incoming.RoomID, incoming.SenderKey, incoming.SessionID)
	if existing, ok := state.MegolmInboundSessions[key]; ok && existing.FirstKnownIndex <= incoming.FirstKnownIndex {
		return state, nil
	}
	state.MegolmInboundSessions[key] = incoming
	return state, nil
}

func MegolmInboundSessionFor(state CryptoState, senderKey, sessionID string) (string, MegolmInboundState, bool) {
	senderKey = strings.TrimSpace(senderKey)
	sessionID = strings.TrimSpace(sessionID)
	for key, session := range state.MegolmInboundSessions {
		if session.SessionID == sessionID && (senderKey == "" || session.SenderKey == senderKey) {
			return key, session, true
		}
	}
	return "", MegolmInboundState{}, false
}

func MegolmInboundSessionKey(roomID, senderKey, sessionID string) string {
	return strings.TrimSpace(roomID) + "/" + strings.TrimSpace(senderKey) + "/" + strings.TrimSpace(sessionID)
}

func decodeOlmPayload(plaintext []byte) (OlmPayload, error) {
	var payload OlmPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return OlmPayload{}, err
	}
	payload.Type = strings.TrimSpace(payload.Type)
	payload.Sender = strings.TrimSpace(payload.Sender)
	payload.Recipient = strings.TrimSpace(payload.Recipient)
	if payload.Type == "" || payload.Sender == "" || payload.Recipient == "" || len(payload.Keys) == 0 || len(payload.RecipientKeys) == 0 {
		return OlmPayload{}, errors.New("matrix olm payload incomplete")
	}
	return payload, nil
}

func ownCurve25519(state CryptoState) string {
	return deviceKeyValue(state.DeviceKeys, "curve25519")
}
