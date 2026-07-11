package matrix

import (
	"errors"
	"strings"
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
