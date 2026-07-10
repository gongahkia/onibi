package matrix

import (
	"errors"
	"strings"

	goolmaccount "maunium.net/go/mautrix/crypto/goolm/account"
	"maunium.net/go/mautrix/id"
)

func NewOlmAccountState(userID, deviceID string, pickleKey []byte, oneTimeKeyCount uint) (CryptoState, error) {
	userID = strings.TrimSpace(userID)
	deviceID = strings.TrimSpace(deviceID)
	if userID == "" || deviceID == "" {
		return CryptoState{}, errors.New("matrix olm account user and device required")
	}
	if len(pickleKey) == 0 {
		return CryptoState{}, errors.New("matrix olm pickle key required")
	}
	acc, err := goolmaccount.NewAccount()
	if err != nil {
		return CryptoState{}, err
	}
	if oneTimeKeyCount > 0 {
		if err := acc.GenOneTimeKeys(oneTimeKeyCount); err != nil {
			return CryptoState{}, err
		}
	}
	pickled, err := acc.Pickle(pickleKey)
	if err != nil {
		return CryptoState{}, err
	}
	ed25519, curve25519, err := acc.IdentityKeys()
	if err != nil {
		return CryptoState{}, err
	}
	otks, err := acc.OneTimeKeys()
	if err != nil {
		return CryptoState{}, err
	}
	return CryptoState{
		UserID:        userID,
		DeviceID:      deviceID,
		AccountPickle: string(pickled),
		DeviceKeys: &DeviceKeys{
			UserID:     userID,
			DeviceID:   deviceID,
			Algorithms: []string{AlgorithmOlmV1, AlgorithmMegolmV1},
			Keys: map[string]string{
				"curve25519:" + deviceID: string(curve25519),
				"ed25519:" + deviceID:    string(ed25519),
			},
		},
		OneTimeKeyCounts: map[string]int{KeyAlgorithmSignedCurve255: len(otks)},
	}, nil
}

func OlmAccountOneTimeKeys(state CryptoState, pickleKey []byte) (map[string]string, error) {
	acc, err := loadOlmAccount(state, pickleKey)
	if err != nil {
		return nil, err
	}
	otks, err := acc.OneTimeKeys()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(otks))
	for keyID, key := range otks {
		out[keyID] = string(key)
	}
	return out, nil
}

func EncryptOlmForDevice(state CryptoState, pickleKey []byte, recipientUserID, recipientDeviceID, recipientCurve25519, recipientOneTimeKey string, plaintext []byte) (CryptoState, OlmSessionState, OlmEncryptedContent, error) {
	recipientUserID = strings.TrimSpace(recipientUserID)
	recipientDeviceID = strings.TrimSpace(recipientDeviceID)
	recipientCurve25519 = strings.TrimSpace(recipientCurve25519)
	recipientOneTimeKey = strings.TrimSpace(recipientOneTimeKey)
	if recipientUserID == "" || recipientDeviceID == "" || recipientCurve25519 == "" || recipientOneTimeKey == "" {
		return state, OlmSessionState{}, OlmEncryptedContent{}, errors.New("matrix olm recipient device keys required")
	}
	if len(plaintext) == 0 {
		return state, OlmSessionState{}, OlmEncryptedContent{}, errors.New("matrix olm plaintext required")
	}
	acc, err := loadOlmAccount(state, pickleKey)
	if err != nil {
		return state, OlmSessionState{}, OlmEncryptedContent{}, err
	}
	_, senderCurve, err := acc.IdentityKeys()
	if err != nil {
		return state, OlmSessionState{}, OlmEncryptedContent{}, err
	}
	sess, err := acc.NewOutboundSession(id.Curve25519(recipientCurve25519), id.Curve25519(recipientOneTimeKey))
	if err != nil {
		return state, OlmSessionState{}, OlmEncryptedContent{}, err
	}
	msgType, body, err := sess.Encrypt(plaintext)
	if err != nil {
		return state, OlmSessionState{}, OlmEncryptedContent{}, err
	}
	pickledSession, err := sess.Pickle(pickleKey)
	if err != nil {
		return state, OlmSessionState{}, OlmEncryptedContent{}, err
	}
	content, err := NewOlmEncryptedContent(string(senderCurve), recipientCurve25519, string(body), int(msgType))
	if err != nil {
		return state, OlmSessionState{}, OlmEncryptedContent{}, err
	}
	return state, OlmSessionState{
		UserID:    recipientUserID,
		DeviceID:  recipientDeviceID,
		SenderKey: recipientCurve25519,
		SessionID: string(sess.ID()),
		Pickle:    string(pickledSession),
	}, content, nil
}

func DecryptOlmFromDevice(state CryptoState, pickleKey []byte, content OlmEncryptedContent) (CryptoState, OlmSessionState, []byte, error) {
	if strings.TrimSpace(content.Algorithm) != AlgorithmOlmV1 {
		return state, OlmSessionState{}, nil, errors.New("matrix olm algorithm mismatch")
	}
	if strings.TrimSpace(content.SenderKey) == "" {
		return state, OlmSessionState{}, nil, errors.New("matrix olm sender key required")
	}
	acc, err := loadOlmAccount(state, pickleKey)
	if err != nil {
		return state, OlmSessionState{}, nil, err
	}
	_, ownCurve, err := acc.IdentityKeys()
	if err != nil {
		return state, OlmSessionState{}, nil, err
	}
	info, ok := content.Ciphertext[string(ownCurve)]
	if !ok || strings.TrimSpace(info.Body) == "" {
		return state, OlmSessionState{}, nil, errors.New("matrix olm ciphertext for device missing")
	}
	if info.Type != OlmMessageTypePreKey {
		return state, OlmSessionState{}, nil, errors.New("matrix olm pre-key message required")
	}
	sender := id.Curve25519(content.SenderKey)
	sess, err := acc.NewInboundSessionFrom(&sender, info.Body)
	if err != nil {
		return state, OlmSessionState{}, nil, err
	}
	plaintext, err := sess.Decrypt(info.Body, id.OlmMsgType(info.Type))
	if err != nil {
		return state, OlmSessionState{}, nil, err
	}
	if err := acc.RemoveOneTimeKeys(sess); err != nil {
		return state, OlmSessionState{}, nil, err
	}
	accountPickle, err := acc.Pickle(pickleKey)
	if err != nil {
		return state, OlmSessionState{}, nil, err
	}
	sessionPickle, err := sess.Pickle(pickleKey)
	if err != nil {
		return state, OlmSessionState{}, nil, err
	}
	otks, err := acc.OneTimeKeys()
	if err != nil {
		return state, OlmSessionState{}, nil, err
	}
	state.AccountPickle = string(accountPickle)
	if state.OneTimeKeyCounts == nil {
		state.OneTimeKeyCounts = map[string]int{}
	}
	state.OneTimeKeyCounts[KeyAlgorithmSignedCurve255] = len(otks)
	return state, OlmSessionState{
		UserID:    state.UserID,
		DeviceID:  state.DeviceID,
		SenderKey: strings.TrimSpace(content.SenderKey),
		SessionID: string(sess.ID()),
		Pickle:    string(sessionPickle),
	}, plaintext, nil
}

func loadOlmAccount(state CryptoState, pickleKey []byte) (*goolmaccount.Account, error) {
	if len(pickleKey) == 0 {
		return nil, errors.New("matrix olm pickle key required")
	}
	if strings.TrimSpace(state.AccountPickle) == "" {
		return nil, errors.New("matrix olm account pickle required")
	}
	return goolmaccount.AccountFromPickled([]byte(state.AccountPickle), pickleKey)
}
