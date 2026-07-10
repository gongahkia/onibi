package matrix

import (
	"errors"
	"strings"
)

const (
	EventRoomEncrypted  = "m.room.encrypted"
	EventRoomKey        = "m.room_key"
	EventRoomKeyRequest = "m.room_key_request"

	RoomKeyActionRequest      = "request"
	RoomKeyActionCancellation = "request_cancellation"

	OlmMessageTypePreKey  = 0
	OlmMessageTypeMessage = 1
)

type MegolmEncryptedContent struct {
	Algorithm  string `json:"algorithm"`
	Ciphertext string `json:"ciphertext"`
	DeviceID   string `json:"device_id,omitempty"`
	SenderKey  string `json:"sender_key,omitempty"`
	SessionID  string `json:"session_id"`
}

type OlmEncryptedContent struct {
	Algorithm  string                       `json:"algorithm"`
	Ciphertext map[string]OlmCiphertextInfo `json:"ciphertext"`
	SenderKey  string                       `json:"sender_key"`
}

type OlmCiphertextInfo struct {
	Body string `json:"body"`
	Type int    `json:"type"`
}

type RoomKeyContent struct {
	Algorithm     string `json:"algorithm"`
	RoomID        string `json:"room_id"`
	SessionID     string `json:"session_id"`
	SessionKey    string `json:"session_key"`
	SharedHistory *bool  `json:"shared_history,omitempty"`
}

type RoomKeyRequestContent struct {
	Action             string            `json:"action"`
	Body               *RequestedKeyInfo `json:"body,omitempty"`
	RequestID          string            `json:"request_id"`
	RequestingDeviceID string            `json:"requesting_device_id"`
}

type RequestedKeyInfo struct {
	Algorithm string `json:"algorithm"`
	RoomID    string `json:"room_id"`
	SenderKey string `json:"sender_key,omitempty"`
	SessionID string `json:"session_id"`
}

func NewMegolmEncryptedContent(senderKey, deviceID, sessionID, ciphertext string) (MegolmEncryptedContent, error) {
	senderKey = strings.TrimSpace(senderKey)
	deviceID = strings.TrimSpace(deviceID)
	sessionID = strings.TrimSpace(sessionID)
	ciphertext = strings.TrimSpace(ciphertext)
	if sessionID == "" || ciphertext == "" {
		return MegolmEncryptedContent{}, errors.New("matrix megolm session id and ciphertext required")
	}
	return MegolmEncryptedContent{
		Algorithm:  AlgorithmMegolmV1,
		Ciphertext: ciphertext,
		DeviceID:   deviceID,
		SenderKey:  senderKey,
		SessionID:  sessionID,
	}, nil
}

func NewOlmEncryptedContent(senderKey, recipientCurve25519Key, body string, messageType int) (OlmEncryptedContent, error) {
	senderKey = strings.TrimSpace(senderKey)
	recipientCurve25519Key = strings.TrimSpace(recipientCurve25519Key)
	body = strings.TrimSpace(body)
	if senderKey == "" || recipientCurve25519Key == "" || body == "" {
		return OlmEncryptedContent{}, errors.New("matrix olm sender key, recipient key, and body required")
	}
	return OlmEncryptedContent{
		Algorithm: AlgorithmOlmV1,
		Ciphertext: map[string]OlmCiphertextInfo{
			recipientCurve25519Key: {Body: body, Type: messageType},
		},
		SenderKey: senderKey,
	}, nil
}

func NewRoomKeyContent(roomID, sessionID, sessionKey string, sharedHistory bool) (RoomKeyContent, error) {
	roomID = strings.TrimSpace(roomID)
	sessionID = strings.TrimSpace(sessionID)
	sessionKey = strings.TrimSpace(sessionKey)
	if roomID == "" || sessionID == "" || sessionKey == "" {
		return RoomKeyContent{}, errors.New("matrix room key room id, session id, and session key required")
	}
	return RoomKeyContent{
		Algorithm:     AlgorithmMegolmV1,
		RoomID:        roomID,
		SessionID:     sessionID,
		SessionKey:    sessionKey,
		SharedHistory: &sharedHistory,
	}, nil
}

func NewRoomKeyRequest(requestingDeviceID, requestID string, body RequestedKeyInfo) (RoomKeyRequestContent, error) {
	requestingDeviceID = strings.TrimSpace(requestingDeviceID)
	requestID = strings.TrimSpace(requestID)
	body.Algorithm = strings.TrimSpace(body.Algorithm)
	body.RoomID = strings.TrimSpace(body.RoomID)
	body.SessionID = strings.TrimSpace(body.SessionID)
	body.SenderKey = strings.TrimSpace(body.SenderKey)
	if body.Algorithm == "" {
		body.Algorithm = AlgorithmMegolmV1
	}
	if requestingDeviceID == "" || requestID == "" || body.RoomID == "" || body.SessionID == "" {
		return RoomKeyRequestContent{}, errors.New("matrix room key request device, request id, room id, and session id required")
	}
	return RoomKeyRequestContent{
		Action:             RoomKeyActionRequest,
		Body:               &body,
		RequestID:          requestID,
		RequestingDeviceID: requestingDeviceID,
	}, nil
}

func NewRoomKeyRequestCancellation(requestingDeviceID, requestID string) (RoomKeyRequestContent, error) {
	requestingDeviceID = strings.TrimSpace(requestingDeviceID)
	requestID = strings.TrimSpace(requestID)
	if requestingDeviceID == "" || requestID == "" {
		return RoomKeyRequestContent{}, errors.New("matrix room key cancellation device and request id required")
	}
	return RoomKeyRequestContent{
		Action:             RoomKeyActionCancellation,
		RequestID:          requestID,
		RequestingDeviceID: requestingDeviceID,
	}, nil
}
