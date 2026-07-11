package matrix

import (
	"errors"
	"fmt"
	"strings"

	"maunium.net/go/mautrix/crypto/signatures"
	"maunium.net/go/mautrix/id"
)

func MarkDeviceTrusted(state CryptoState, userID, deviceID string) (CryptoState, error) {
	userID = strings.TrimSpace(userID)
	deviceID = strings.TrimSpace(deviceID)
	if userID == "" || deviceID == "" {
		return state, errors.New("matrix trusted device user and device required")
	}
	if state.TrustedDevices == nil {
		state.TrustedDevices = map[string][]string{}
	}
	state.TrustedDevices[userID] = appendUniqueString(state.TrustedDevices[userID], deviceID)
	return state, nil
}

func MarkDeviceTrustedFromSAS(state CryptoState, transactionID string) (CryptoState, error) {
	transactionID = strings.TrimSpace(transactionID)
	if transactionID == "" {
		return state, errors.New("matrix trusted device transaction required")
	}
	txn, ok := state.SASTransactions[transactionID]
	if !ok {
		return state, errors.New("matrix trusted device SAS transaction missing")
	}
	if txn.State != SASStateDone {
		return state, errors.New("matrix trusted device SAS transaction not complete")
	}
	return MarkDeviceTrusted(state, txn.UserID, txn.DeviceID)
}

func IsDeviceTrusted(state CryptoState, userID, deviceID string) bool {
	userID = strings.TrimSpace(userID)
	deviceID = strings.TrimSpace(deviceID)
	if userID == "" || deviceID == "" {
		return false
	}
	for _, trusted := range state.TrustedDevices[userID] {
		if strings.TrimSpace(trusted) == deviceID {
			return true
		}
	}
	return false
}

func TrustedDeviceKeyFor(state CryptoState, userID, deviceID string) (TrustedDeviceKey, bool) {
	key, ok := state.TrustedDeviceKeys[trustedDeviceKey(userID, deviceID)]
	return key, ok
}

func PinTrustedDevice(state CryptoState, keys DeviceKeys) (CryptoState, error) {
	keys.UserID = strings.TrimSpace(keys.UserID)
	keys.DeviceID = strings.TrimSpace(keys.DeviceID)
	curve25519 := deviceKeyValue(&keys, "curve25519")
	ed25519 := deviceKeyValue(&keys, "ed25519")
	if keys.UserID == "" || keys.DeviceID == "" || curve25519 == "" || ed25519 == "" || !roomKeyShareContainsString(keys.Algorithms, AlgorithmOlmV1) || !roomKeyShareContainsString(keys.Algorithms, AlgorithmMegolmV1) {
		return state, errors.New("matrix trusted device keys incomplete")
	}
	ok, err := signatures.VerifySignatureJSON(keys, id.UserID(keys.UserID), keys.DeviceID, id.Ed25519(ed25519))
	if err != nil || !ok {
		if err != nil {
			return state, fmt.Errorf("matrix trusted device signature: %w", err)
		}
		return state, errors.New("matrix trusted device signature invalid")
	}
	pinned := TrustedDeviceKey{UserID: keys.UserID, DeviceID: keys.DeviceID, Curve25519Key: curve25519, Ed25519Key: ed25519}
	if state.TrustedDeviceKeys == nil {
		state.TrustedDeviceKeys = map[string]TrustedDeviceKey{}
	}
	key := trustedDeviceKey(keys.UserID, keys.DeviceID)
	if existing, ok := state.TrustedDeviceKeys[key]; ok && existing != pinned {
		return state, errors.New("matrix trusted device key changed; repeat SAS verification before replacing it")
	}
	state.TrustedDeviceKeys[key] = pinned
	return MarkDeviceTrusted(state, keys.UserID, keys.DeviceID)
}

func IsTrustedDeviceEvent(state CryptoState, userID, deviceID, curve25519, ed25519 string) bool {
	pinned, ok := TrustedDeviceKeyFor(state, userID, deviceID)
	return ok && IsDeviceTrusted(state, userID, deviceID) && pinned.Curve25519Key == strings.TrimSpace(curve25519) && pinned.Ed25519Key == strings.TrimSpace(ed25519)
}

func trustedDeviceKey(userID, deviceID string) string {
	return strings.TrimSpace(userID) + "/" + strings.TrimSpace(deviceID)
}

func TrustedRoomKeyShareTargets(state CryptoState, targets []RoomKeyShareTarget) ([]RoomKeyShareTarget, error) {
	targets, err := normalizeRoomKeyShareTargets(targets)
	if err != nil {
		return nil, err
	}
	out := make([]RoomKeyShareTarget, 0, len(targets))
	for _, target := range targets {
		if IsDeviceTrusted(state, target.UserID, target.DeviceID) {
			out = append(out, target)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("matrix room key share requires trusted devices")
	}
	return out, nil
}
