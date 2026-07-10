package matrix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type MegolmPayload struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
	RoomID  string          `json:"room_id"`
}

func EncryptMegolmRoomEvent(state MegolmOutboundState, pickleKey []byte, senderKey, deviceID, roomID, eventType string, content any) (MegolmOutboundState, MegolmEncryptedContent, error) {
	roomID = strings.TrimSpace(roomID)
	eventType = strings.TrimSpace(eventType)
	if roomID == "" || eventType == "" {
		return state, MegolmEncryptedContent{}, errors.New("matrix megolm room id and event type required")
	}
	if state.RoomID != "" && state.RoomID != roomID {
		return state, MegolmEncryptedContent{}, errors.New("matrix megolm room mismatch")
	}
	contentBytes, err := json.Marshal(content)
	if err != nil {
		return state, MegolmEncryptedContent{}, err
	}
	payload, err := json.Marshal(MegolmPayload{
		Type:    eventType,
		Content: contentBytes,
		RoomID:  roomID,
	})
	if err != nil {
		return state, MegolmEncryptedContent{}, err
	}
	return EncryptMegolmState(state, pickleKey, senderKey, deviceID, payload)
}

func DecryptMegolmRoomEvent(state MegolmInboundState, pickleKey []byte, content MegolmEncryptedContent, roomID string) (MegolmInboundState, MegolmPayload, uint, error) {
	nextState, plaintext, index, err := DecryptMegolmState(state, pickleKey, content)
	if err != nil {
		return state, MegolmPayload{}, 0, err
	}
	var payload MegolmPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return state, MegolmPayload{}, 0, err
	}
	payload.Type = strings.TrimSpace(payload.Type)
	payload.RoomID = strings.TrimSpace(payload.RoomID)
	roomID = strings.TrimSpace(roomID)
	if payload.Type == "" || len(payload.Content) == 0 || payload.RoomID == "" {
		return state, MegolmPayload{}, 0, errors.New("matrix megolm payload incomplete")
	}
	if state.RoomID != "" && payload.RoomID != state.RoomID {
		return state, MegolmPayload{}, 0, errors.New("matrix megolm payload room mismatch")
	}
	if roomID != "" && payload.RoomID != roomID {
		return state, MegolmPayload{}, 0, errors.New("matrix megolm payload requested room mismatch")
	}
	return nextState, payload, index, nil
}

func (c *Client) SendMegolmEncryptedEvent(ctx context.Context, roomID string, content MegolmEncryptedContent) (string, error) {
	if c == nil {
		return "", errors.New("matrix client nil")
	}
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return "", errors.New("matrix room id required")
	}
	if strings.TrimSpace(content.Algorithm) != AlgorithmMegolmV1 || strings.TrimSpace(content.SessionID) == "" || strings.TrimSpace(content.Ciphertext) == "" {
		return "", errors.New("matrix megolm encrypted content incomplete")
	}
	txnID := fmt.Sprintf("%d", time.Now().UnixNano())
	if c.TxnID != nil {
		txnID = c.TxnID()
	}
	p := "/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/send/" + url.PathEscape(EventRoomEncrypted) + "/" + url.PathEscape(txnID)
	var out SendResponse
	if err := c.do(ctx, http.MethodPut, p, content, &out); err != nil {
		return "", err
	}
	return out.EventID, nil
}
