package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	StreamIDBytes  = 16
	RelayNonceSize = 12
	streamInfoBase = "onibi-e2e-stream-v1:"
	nonceInfoBase  = "onibi-e2e-nonce-v1:"
)

type RelayFrame struct {
	Version   string `json:"v"`
	SessionID string `json:"sid"`
	StreamID  string `json:"st"`
	Channel   string `json:"ch"`
	Direction string `json:"dir"`
	Seq       uint64 `json:"seq"`
	IV        string `json:"iv"`
	Type      string `json:"t"`
	Data      string `json:"ct"`
}

func NewStreamID() (string, error) {
	raw := make([]byte, StreamIDBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func SealRelayFrame(sessionKey []byte, sessionID, streamID, channel, dir string, seq uint64, typ string, plaintext []byte) ([]byte, error) {
	if len(sessionKey) != KeyBytes {
		return nil, fmt.Errorf("e2e session key must be %d bytes", KeyBytes)
	}
	if sessionID == "" || streamID == "" || channel == "" || dir == "" || typ == "" {
		return nil, errors.New("e2e frame metadata required")
	}
	aead, iv, err := relayAEAD(sessionKey, streamID, channel, dir, seq)
	if err != nil {
		return nil, err
	}
	frame := RelayFrame{
		Version:   Version,
		SessionID: sessionID,
		StreamID:  streamID,
		Channel:   channel,
		Direction: dir,
		Seq:       seq,
		IV:        base64.RawURLEncoding.EncodeToString(iv),
		Type:      typ,
	}
	frame.Data = base64.RawURLEncoding.EncodeToString(aead.Seal(nil, iv, plaintext, RelayAAD(frame)))
	return json.Marshal(frame)
}

func OpenRelayFrame(sessionKey []byte, expectedSessionID, expectedChannel, expectedDir string, expectedSeq uint64, frameBytes []byte) (RelayFrame, []byte, error) {
	if len(sessionKey) != KeyBytes {
		return RelayFrame{}, nil, fmt.Errorf("e2e session key must be %d bytes", KeyBytes)
	}
	var frame RelayFrame
	if err := json.Unmarshal(frameBytes, &frame); err != nil {
		return RelayFrame{}, nil, err
	}
	if frame.Version != Version {
		return RelayFrame{}, nil, errors.New("bad e2e frame version")
	}
	if frame.SessionID != expectedSessionID || frame.Channel != expectedChannel || frame.Direction != expectedDir {
		return RelayFrame{}, nil, errors.New("bad e2e frame binding")
	}
	if frame.Seq != expectedSeq {
		return RelayFrame{}, nil, errors.New("bad e2e frame sequence")
	}
	if frame.Type != "text" && frame.Type != "binary" {
		return RelayFrame{}, nil, fmt.Errorf("bad e2e frame type %q", frame.Type)
	}
	aead, wantIV, err := relayAEAD(sessionKey, frame.StreamID, frame.Channel, frame.Direction, frame.Seq)
	if err != nil {
		return RelayFrame{}, nil, err
	}
	iv, err := base64.RawURLEncoding.DecodeString(frame.IV)
	if err != nil {
		return RelayFrame{}, nil, err
	}
	if len(iv) != RelayNonceSize || string(iv) != string(wantIV) {
		return RelayFrame{}, nil, errors.New("bad e2e frame iv")
	}
	ct, err := base64.RawURLEncoding.DecodeString(frame.Data)
	if err != nil {
		return RelayFrame{}, nil, err
	}
	plaintext, err := aead.Open(nil, iv, ct, RelayAAD(frame))
	if err != nil {
		return RelayFrame{}, nil, err
	}
	return frame, plaintext, nil
}

func RelayAAD(frame RelayFrame) []byte {
	return []byte(strings.Join([]string{
		frame.Version,
		frame.SessionID,
		frame.StreamID,
		frame.Channel,
		frame.Direction,
		strconv.FormatUint(frame.Seq, 10),
		frame.IV,
		frame.Type,
	}, "\n"))
}

func relayAEAD(sessionKey []byte, streamID, channel, dir string, seq uint64) (cipher.AEAD, []byte, error) {
	streamRaw, err := base64.RawURLEncoding.DecodeString(streamID)
	if err != nil {
		return nil, nil, err
	}
	if len(streamRaw) != StreamIDBytes {
		return nil, nil, errors.New("bad e2e stream id")
	}
	key := hkdfSHA256(sessionKey, streamRaw, []byte(streamInfoBase+channel+":"+dir), aesKeyBytes)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	prefix := hkdfSHA256(sessionKey, streamRaw, []byte(nonceInfoBase+channel+":"+dir), 4)
	iv := make([]byte, RelayNonceSize)
	copy(iv, prefix)
	binary.BigEndian.PutUint64(iv[4:], seq)
	return aead, iv, nil
}
