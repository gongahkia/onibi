package matrix

import (
	"context"
	"errors"
	"strings"

	"maunium.net/go/mautrix/crypto/canonicaljson"
)

type SignedOneTimeKey struct {
	Key        string                       `json:"key"`
	Signatures map[string]map[string]string `json:"signatures,omitempty"`
}

func (c *Client) UploadCryptoKeys(ctx context.Context, state CryptoState, pickleKey []byte, uploadDeviceKeys bool) (CryptoState, KeysUploadResponse, error) {
	if c == nil {
		return state, KeysUploadResponse{}, errors.New("matrix client nil")
	}
	acc, err := loadOlmAccount(state, pickleKey)
	if err != nil {
		return state, KeysUploadResponse{}, err
	}
	req := KeysUploadRequest{}
	if uploadDeviceKeys {
		deviceKeys, err := SignedDeviceKeys(state, pickleKey)
		if err != nil {
			return state, KeysUploadResponse{}, err
		}
		req.DeviceKeys = &deviceKeys
	}
	oneTimeKeys, err := SignedOneTimeKeys(state, pickleKey)
	if err != nil {
		return state, KeysUploadResponse{}, err
	}
	req.OneTimeKeys = oneTimeKeys
	resp, err := c.UploadKeys(ctx, req)
	if err != nil {
		return state, KeysUploadResponse{}, err
	}
	if len(oneTimeKeys) > 0 {
		acc.MarkKeysAsPublished()
		pickled, err := acc.Pickle(pickleKey)
		if err != nil {
			return state, KeysUploadResponse{}, err
		}
		state.AccountPickle = string(pickled)
	}
	if uploadDeviceKeys {
		state.AccountShared = true
	}
	if resp.OneTimeKeyCounts != nil {
		state.OneTimeKeyCounts = resp.OneTimeKeyCounts
	}
	return state, resp, nil
}

func SignedDeviceKeys(state CryptoState, pickleKey []byte) (DeviceKeys, error) {
	if state.DeviceKeys == nil {
		return DeviceKeys{}, errors.New("matrix device keys required")
	}
	keys := *state.DeviceKeys
	keys.Signatures = nil
	if strings.TrimSpace(keys.UserID) == "" || strings.TrimSpace(keys.DeviceID) == "" {
		return DeviceKeys{}, errors.New("matrix device keys user and device required")
	}
	sig, err := signCanonicalJSON(state, pickleKey, keys)
	if err != nil {
		return DeviceKeys{}, err
	}
	keys.Signatures = matrixSignature(keys.UserID, keys.DeviceID, sig)
	return keys, nil
}

func SignedOneTimeKeys(state CryptoState, pickleKey []byte) (map[string]any, error) {
	acc, err := loadOlmAccount(state, pickleKey)
	if err != nil {
		return nil, err
	}
	otks, err := acc.OneTimeKeys()
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(otks))
	for keyID, key := range otks {
		body := SignedOneTimeKey{Key: string(key)}
		sig, err := signCanonicalJSON(state, pickleKey, body)
		if err != nil {
			return nil, err
		}
		body.Signatures = matrixSignature(state.UserID, state.DeviceID, sig)
		out[KeyAlgorithmSignedCurve255+":"+keyID] = body
	}
	return out, nil
}

func signCanonicalJSON(state CryptoState, pickleKey []byte, value any) (string, error) {
	acc, err := loadOlmAccount(state, pickleKey)
	if err != nil {
		return "", err
	}
	raw, err := canonicaljson.Marshal(value)
	if err != nil {
		return "", err
	}
	sig, err := acc.Sign(raw)
	if err != nil {
		return "", err
	}
	return string(sig), nil
}

func matrixSignature(userID, deviceID, sig string) map[string]map[string]string {
	return map[string]map[string]string{
		userID: {"ed25519:" + deviceID: sig},
	}
}
