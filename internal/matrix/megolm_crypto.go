package matrix

import (
	"errors"
	"strings"

	goolmsession "maunium.net/go/mautrix/crypto/goolm/session"
)

func NewMegolmOutboundState(roomID string, pickleKey []byte) (MegolmOutboundState, RoomKeyContent, error) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return MegolmOutboundState{}, RoomKeyContent{}, errors.New("matrix megolm room id required")
	}
	if len(pickleKey) == 0 {
		return MegolmOutboundState{}, RoomKeyContent{}, errors.New("matrix megolm pickle key required")
	}
	sess, err := goolmsession.NewMegolmOutboundSession()
	if err != nil {
		return MegolmOutboundState{}, RoomKeyContent{}, err
	}
	sessionID := string(sess.ID())
	roomKey, err := NewRoomKeyContent(roomID, sessionID, sess.Key(), false)
	if err != nil {
		return MegolmOutboundState{}, RoomKeyContent{}, err
	}
	pickled, err := sess.Pickle(pickleKey)
	if err != nil {
		return MegolmOutboundState{}, RoomKeyContent{}, err
	}
	return MegolmOutboundState{
		RoomID:       roomID,
		SessionID:    sessionID,
		Pickle:       string(pickled),
		MessageIndex: int(sess.MessageIndex()),
		SharedWith:   map[string][]string{},
	}, roomKey, nil
}

func EncryptMegolmState(state MegolmOutboundState, pickleKey []byte, senderKey, deviceID string, plaintext []byte) (MegolmOutboundState, MegolmEncryptedContent, error) {
	if len(pickleKey) == 0 {
		return state, MegolmEncryptedContent{}, errors.New("matrix megolm pickle key required")
	}
	if strings.TrimSpace(state.Pickle) == "" || strings.TrimSpace(state.SessionID) == "" {
		return state, MegolmEncryptedContent{}, errors.New("matrix megolm outbound state incomplete")
	}
	sess, err := goolmsession.MegolmOutboundSessionFromPickled([]byte(state.Pickle), pickleKey)
	if err != nil {
		return state, MegolmEncryptedContent{}, err
	}
	if string(sess.ID()) != state.SessionID {
		return state, MegolmEncryptedContent{}, errors.New("matrix megolm outbound session id mismatch")
	}
	ciphertext, err := sess.Encrypt(plaintext)
	if err != nil {
		return state, MegolmEncryptedContent{}, err
	}
	pickled, err := sess.Pickle(pickleKey)
	if err != nil {
		return state, MegolmEncryptedContent{}, err
	}
	state.Pickle = string(pickled)
	state.MessageIndex = int(sess.MessageIndex())
	content, err := NewMegolmEncryptedContent(senderKey, deviceID, state.SessionID, string(ciphertext))
	if err != nil {
		return state, MegolmEncryptedContent{}, err
	}
	return state, content, nil
}

func NewMegolmInboundState(roomKey RoomKeyContent, senderKey string, pickleKey []byte) (MegolmInboundState, error) {
	if len(pickleKey) == 0 {
		return MegolmInboundState{}, errors.New("matrix megolm pickle key required")
	}
	if strings.TrimSpace(roomKey.Algorithm) != AlgorithmMegolmV1 || strings.TrimSpace(roomKey.RoomID) == "" || strings.TrimSpace(roomKey.SessionID) == "" || strings.TrimSpace(roomKey.SessionKey) == "" {
		return MegolmInboundState{}, errors.New("matrix megolm room key incomplete")
	}
	sess, err := goolmsession.NewMegolmInboundSession([]byte(roomKey.SessionKey))
	if err != nil {
		return MegolmInboundState{}, err
	}
	if string(sess.ID()) != roomKey.SessionID {
		return MegolmInboundState{}, errors.New("matrix megolm inbound session id mismatch")
	}
	pickled, err := sess.Pickle(pickleKey)
	if err != nil {
		return MegolmInboundState{}, err
	}
	return MegolmInboundState{
		RoomID:          strings.TrimSpace(roomKey.RoomID),
		SenderKey:       strings.TrimSpace(senderKey),
		SessionID:       string(sess.ID()),
		Pickle:          string(pickled),
		FirstKnownIndex: int(sess.FirstKnownIndex()),
	}, nil
}

func DecryptMegolmState(state MegolmInboundState, pickleKey []byte, content MegolmEncryptedContent) (MegolmInboundState, []byte, uint, error) {
	if len(pickleKey) == 0 {
		return state, nil, 0, errors.New("matrix megolm pickle key required")
	}
	if strings.TrimSpace(content.Algorithm) != AlgorithmMegolmV1 || strings.TrimSpace(content.SessionID) == "" || strings.TrimSpace(content.Ciphertext) == "" {
		return state, nil, 0, errors.New("matrix megolm encrypted content incomplete")
	}
	if strings.TrimSpace(state.Pickle) == "" || strings.TrimSpace(state.SessionID) == "" {
		return state, nil, 0, errors.New("matrix megolm inbound state incomplete")
	}
	if content.SessionID != state.SessionID {
		return state, nil, 0, errors.New("matrix megolm encrypted session id mismatch")
	}
	if state.SenderKey != "" && content.SenderKey != "" && content.SenderKey != state.SenderKey {
		return state, nil, 0, errors.New("matrix megolm sender key mismatch")
	}
	sess, err := goolmsession.MegolmInboundSessionFromPickled([]byte(state.Pickle), pickleKey)
	if err != nil {
		return state, nil, 0, err
	}
	plaintext, index, err := sess.Decrypt([]byte(content.Ciphertext))
	if err != nil {
		return state, nil, 0, err
	}
	pickled, err := sess.Pickle(pickleKey)
	if err != nil {
		return state, nil, 0, err
	}
	state.Pickle = string(pickled)
	state.FirstKnownIndex = int(sess.FirstKnownIndex())
	return state, plaintext, index, nil
}
