package matrix

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"

	"golang.org/x/crypto/hkdf"
)

const (
	EventKeyVerificationRequest = "m.key.verification.request"
	EventKeyVerificationReady   = "m.key.verification.ready"
	EventKeyVerificationStart   = "m.key.verification.start"
	EventKeyVerificationAccept  = "m.key.verification.accept"
	EventKeyVerificationKey     = "m.key.verification.key"
	EventKeyVerificationMAC     = "m.key.verification.mac"
	EventKeyVerificationDone    = "m.key.verification.done"
	EventKeyVerificationCancel  = "m.key.verification.cancel"

	VerificationMethodSASV1 = "m.sas.v1"

	KeyAgreementCurve25519SHA256 = "curve25519-hkdf-sha256"
	KeyAgreementCurve25519       = "curve25519"
	HashSHA256                   = "sha256"
	MACHKDFHMACSHA256V2          = "hkdf-hmac-sha256.v2"
	MACHKDFHMACSHA256            = "hkdf-hmac-sha256"
	SASDecimal                   = "decimal"
	SASEmoji                     = "emoji"

	VerificationCancelUser       = "m.user"
	VerificationCancelTimeout    = "m.timeout"
	VerificationCancelMismatch   = "m.mismatched_sas"
	VerificationCancelUnexpected = "m.unexpected_message"

	SASStateStarted     = "started"
	SASStateAccepted    = "accepted"
	SASStateKeyReceived = "key_received"
	SASStateMACReceived = "mac_received"
	SASStateDone        = "done"
	SASStateCancelled   = "cancelled"
)

type VerificationRelatesTo struct {
	RelType string `json:"rel_type,omitempty"`
	EventID string `json:"event_id,omitempty"`
}

type VerificationStartContent struct {
	FromDevice                 string                 `json:"from_device,omitempty"`
	Method                     string                 `json:"method"`
	TransactionID              string                 `json:"transaction_id,omitempty"`
	KeyAgreementProtocols      []string               `json:"key_agreement_protocols,omitempty"`
	Hashes                     []string               `json:"hashes,omitempty"`
	MessageAuthenticationCodes []string               `json:"message_authentication_codes,omitempty"`
	ShortAuthenticationString  []string               `json:"short_authentication_string,omitempty"`
	RelatesTo                  *VerificationRelatesTo `json:"m.relates_to,omitempty"`
}

type VerificationAcceptContent struct {
	Commitment                string   `json:"commitment"`
	Hash                      string   `json:"hash"`
	KeyAgreementProtocol      string   `json:"key_agreement_protocol"`
	MessageAuthenticationCode string   `json:"message_authentication_code"`
	Method                    string   `json:"method"`
	ShortAuthenticationString []string `json:"short_authentication_string"`
	TransactionID             string   `json:"transaction_id,omitempty"`
}

type VerificationKeyContent struct {
	Key           string `json:"key"`
	TransactionID string `json:"transaction_id,omitempty"`
}

type VerificationMACContent struct {
	Keys          string            `json:"keys"`
	MAC           map[string]string `json:"mac"`
	TransactionID string            `json:"transaction_id,omitempty"`
}

type VerificationDoneContent struct {
	TransactionID string `json:"transaction_id,omitempty"`
}

type VerificationCancelContent struct {
	Code          string `json:"code"`
	Reason        string `json:"reason"`
	TransactionID string `json:"transaction_id,omitempty"`
}

type SASInfo struct {
	StartUserID          string
	StartDeviceID        string
	StartPublicKey       string
	AcceptUserID         string
	AcceptDeviceID       string
	AcceptPublicKey      string
	TransactionID        string
	KeyAgreementProtocol string
}

type SASEmojiValue struct {
	Index       int
	Emoji       string
	Description string
}

type SASMACInfo struct {
	UserID        string
	DeviceID      string
	OtherUserID   string
	OtherDeviceID string
	TransactionID string
}

var sasEmojiTable = [...]SASEmojiValue{
	{0, "\U0001f436", "Dog"},
	{1, "\U0001f431", "Cat"},
	{2, "\U0001f981", "Lion"},
	{3, "\U0001f40e", "Horse"},
	{4, "\U0001f984", "Unicorn"},
	{5, "\U0001f437", "Pig"},
	{6, "\U0001f418", "Elephant"},
	{7, "\U0001f430", "Rabbit"},
	{8, "\U0001f43c", "Panda"},
	{9, "\U0001f413", "Rooster"},
	{10, "\U0001f427", "Penguin"},
	{11, "\U0001f422", "Turtle"},
	{12, "\U0001f41f", "Fish"},
	{13, "\U0001f419", "Octopus"},
	{14, "\U0001f98b", "Butterfly"},
	{15, "\U0001f337", "Flower"},
	{16, "\U0001f333", "Tree"},
	{17, "\U0001f335", "Cactus"},
	{18, "\U0001f344", "Mushroom"},
	{19, "\U0001f30f", "Globe"},
	{20, "\U0001f319", "Moon"},
	{21, "\u2601\ufe0f", "Cloud"},
	{22, "\U0001f525", "Fire"},
	{23, "\U0001f34c", "Banana"},
	{24, "\U0001f34e", "Apple"},
	{25, "\U0001f353", "Strawberry"},
	{26, "\U0001f33d", "Corn"},
	{27, "\U0001f355", "Pizza"},
	{28, "\U0001f382", "Cake"},
	{29, "\u2764\ufe0f", "Heart"},
	{30, "\U0001f600", "Smiley"},
	{31, "\U0001f916", "Robot"},
	{32, "\U0001f3a9", "Hat"},
	{33, "\U0001f453", "Glasses"},
	{34, "\U0001f527", "Spanner"},
	{35, "\U0001f385", "Santa"},
	{36, "\U0001f44d", "Thumbs Up"},
	{37, "\u2602\ufe0f", "Umbrella"},
	{38, "\u231b", "Hourglass"},
	{39, "\u23f0", "Clock"},
	{40, "\U0001f381", "Gift"},
	{41, "\U0001f4a1", "Light Bulb"},
	{42, "\U0001f4d5", "Book"},
	{43, "\u270f\ufe0f", "Pencil"},
	{44, "\U0001f4ce", "Paperclip"},
	{45, "\u2702\ufe0f", "Scissors"},
	{46, "\U0001f512", "Lock"},
	{47, "\U0001f511", "Key"},
	{48, "\U0001f528", "Hammer"},
	{49, "\u260e\ufe0f", "Telephone"},
	{50, "\U0001f3c1", "Flag"},
	{51, "\U0001f682", "Train"},
	{52, "\U0001f6b2", "Bicycle"},
	{53, "\u2708\ufe0f", "Aeroplane"},
	{54, "\U0001f680", "Rocket"},
	{55, "\U0001f3c6", "Trophy"},
	{56, "\u26bd", "Ball"},
	{57, "\U0001f3b8", "Guitar"},
	{58, "\U0001f3ba", "Trumpet"},
	{59, "\U0001f514", "Bell"},
	{60, "\u2693", "Anchor"},
	{61, "\U0001f3a7", "Headphones"},
	{62, "\U0001f4c1", "Folder"},
	{63, "\U0001f4cc", "Pin"},
}

func DefaultSASStart(transactionID, fromDevice string) VerificationStartContent {
	return VerificationStartContent{
		FromDevice:                 strings.TrimSpace(fromDevice),
		Method:                     VerificationMethodSASV1,
		TransactionID:              strings.TrimSpace(transactionID),
		KeyAgreementProtocols:      []string{KeyAgreementCurve25519SHA256, KeyAgreementCurve25519},
		Hashes:                     []string{HashSHA256},
		MessageAuthenticationCodes: []string{MACHKDFHMACSHA256V2, MACHKDFHMACSHA256},
		ShortAuthenticationString:  []string{SASDecimal, SASEmoji},
	}
}

func DefaultSASAccept(transactionID, commitment string) VerificationAcceptContent {
	return VerificationAcceptContent{
		Commitment:                strings.TrimSpace(commitment),
		Hash:                      HashSHA256,
		KeyAgreementProtocol:      KeyAgreementCurve25519SHA256,
		MessageAuthenticationCode: MACHKDFHMACSHA256V2,
		Method:                    VerificationMethodSASV1,
		ShortAuthenticationString: []string{SASDecimal, SASEmoji},
		TransactionID:             strings.TrimSpace(transactionID),
	}
}

func VerificationToDeviceMessages(userID, deviceID string, content any) (map[string]map[string]any, error) {
	userID = strings.TrimSpace(userID)
	deviceID = strings.TrimSpace(deviceID)
	if userID == "" || deviceID == "" {
		return nil, errors.New("matrix verification user and device required")
	}
	if content == nil {
		return nil, errors.New("matrix verification content required")
	}
	return map[string]map[string]any{userID: {deviceID: content}}, nil
}

func (c *Client) SendVerificationToDevice(ctx context.Context, eventType, userID, deviceID string, content any) error {
	messages, err := VerificationToDeviceMessages(userID, deviceID, content)
	if err != nil {
		return err
	}
	return c.SendToDevice(ctx, eventType, messages)
}

func SASCommitment(ephemeralPublicKey string, start VerificationStartContent) (string, error) {
	ephemeralPublicKey = strings.TrimSpace(ephemeralPublicKey)
	if ephemeralPublicKey == "" {
		return "", errors.New("matrix SAS ephemeral key required")
	}
	b, err := json.Marshal(verificationStartCanonicalMap(start))
	if err != nil {
		return "", err
	}
	payload := append([]byte(ephemeralPublicKey), b...)
	sum := sha256.Sum256(payload)
	return base64.RawStdEncoding.EncodeToString(sum[:]), nil
}

func SASBytes(sharedSecret []byte, info SASInfo, n int) ([]byte, error) {
	if len(sharedSecret) == 0 {
		return nil, errors.New("matrix SAS shared secret required")
	}
	if n <= 0 {
		return nil, errors.New("matrix SAS byte count required")
	}
	infoString, err := info.hkdfInfo()
	if err != nil {
		return nil, err
	}
	out := make([]byte, n)
	if _, err := io.ReadFull(hkdf.New(sha256.New, sharedSecret, nil, []byte(infoString)), out); err != nil {
		return nil, err
	}
	return out, nil
}

func SASDecimalSequence(sharedSecret []byte, info SASInfo) ([3]int, error) {
	b, err := SASBytes(sharedSecret, info, 5)
	if err != nil {
		return [3]int{}, err
	}
	return SASDecimalSequenceFromBytes(b)
}

func SASDecimalSequenceFromBytes(b []byte) ([3]int, error) {
	if len(b) < 5 {
		return [3]int{}, errors.New("matrix SAS decimal requires 5 bytes")
	}
	return [3]int{
		((int(b[0]) << 5) | (int(b[1]) >> 3)) + 1000,
		(((int(b[1]) & 0x7) << 10) | (int(b[2]) << 2) | (int(b[3]) >> 6)) + 1000,
		(((int(b[3]) & 0x3f) << 7) | (int(b[4]) >> 1)) + 1000,
	}, nil
}

func SASEmojiSequence(sharedSecret []byte, info SASInfo) ([7]SASEmojiValue, error) {
	b, err := SASBytes(sharedSecret, info, 6)
	if err != nil {
		return [7]SASEmojiValue{}, err
	}
	return SASEmojiSequenceFromBytes(b)
}

func SASEmojiSequenceFromBytes(b []byte) ([7]SASEmojiValue, error) {
	if len(b) < 6 {
		return [7]SASEmojiValue{}, errors.New("matrix SAS emoji requires 6 bytes")
	}
	indices := [7]int{
		int(b[0] >> 2),
		int(((b[0] & 0x03) << 4) | (b[1] >> 4)),
		int(((b[1] & 0x0f) << 2) | (b[2] >> 6)),
		int(b[2] & 0x3f),
		int(b[3] >> 2),
		int(((b[3] & 0x03) << 4) | (b[4] >> 4)),
		int(((b[4] & 0x0f) << 2) | (b[5] >> 6)),
	}
	var out [7]SASEmojiValue
	for i, idx := range indices {
		out[i] = sasEmojiTable[idx]
	}
	return out, nil
}

func BuildSASMACContent(sharedSecret []byte, info SASMACInfo, keys map[string]string) (VerificationMACContent, error) {
	keyIDs := sortedKeyIDs(keys)
	if len(keyIDs) == 0 {
		return VerificationMACContent{}, errors.New("matrix SAS MAC keys required")
	}
	macs := make(map[string]string, len(keyIDs))
	for _, keyID := range keyIDs {
		mac, err := SASMACV2(sharedSecret, info, keyID, keys[keyID])
		if err != nil {
			return VerificationMACContent{}, err
		}
		macs[keyID] = mac
	}
	keysMAC, err := SASMACV2(sharedSecret, info, "KEY_IDS", strings.Join(keyIDs, ","))
	if err != nil {
		return VerificationMACContent{}, err
	}
	return VerificationMACContent{TransactionID: strings.TrimSpace(info.TransactionID), MAC: macs, Keys: keysMAC}, nil
}

func VerifySASMACContent(sharedSecret []byte, info SASMACInfo, content VerificationMACContent, keys map[string]string) error {
	if strings.TrimSpace(content.TransactionID) == "" || strings.TrimSpace(content.TransactionID) != strings.TrimSpace(info.TransactionID) {
		return errors.New("matrix SAS MAC transaction mismatch")
	}
	keyIDs := sortedKeyIDs(content.MAC)
	if len(keyIDs) == 0 {
		return errors.New("matrix SAS MAC keys required")
	}
	wantKeys, err := SASMACV2(sharedSecret, info, "KEY_IDS", strings.Join(keyIDs, ","))
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(content.Keys), []byte(wantKeys)) {
		return errors.New("matrix SAS MAC key list mismatch")
	}
	for _, keyID := range keyIDs {
		keyValue, ok := keys[keyID]
		if !ok {
			return errors.New("matrix SAS MAC unknown key id")
		}
		want, err := SASMACV2(sharedSecret, info, keyID, keyValue)
		if err != nil {
			return err
		}
		if !hmac.Equal([]byte(content.MAC[keyID]), []byte(want)) {
			return errors.New("matrix SAS MAC key mismatch")
		}
	}
	return nil
}

func SASMACV2(sharedSecret []byte, info SASMACInfo, keyID, value string) (string, error) {
	if len(sharedSecret) == 0 {
		return "", errors.New("matrix SAS shared secret required")
	}
	infoString, err := info.hkdfInfo(keyID)
	if err != nil {
		return "", err
	}
	k := make([]byte, sha256.Size)
	if _, err := io.ReadFull(hkdf.New(sha256.New, sharedSecret, nil, []byte(infoString)), k); err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, k)
	_, _ = mac.Write([]byte(value))
	return base64.RawStdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s SASTransactionState) ApplyVerificationEvent(eventType string, raw json.RawMessage) (SASTransactionState, error) {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return s, errors.New("matrix verification event type required")
	}
	next := s
	switch eventType {
	case EventKeyVerificationStart:
		if s.State != "" && s.State != SASStateStarted {
			return s, verificationTransitionError(s.State, eventType)
		}
		var content VerificationStartContent
		if err := json.Unmarshal(raw, &content); err != nil {
			return s, err
		}
		if err := applyVerificationTransaction(&next, content.TransactionID); err != nil {
			return s, err
		}
		next.DeviceID = firstNonEmptyString(next.DeviceID, content.FromDevice)
		next.State = SASStateStarted
	case EventKeyVerificationAccept:
		if s.State != SASStateStarted {
			return s, verificationTransitionError(s.State, eventType)
		}
		var content VerificationAcceptContent
		if err := json.Unmarshal(raw, &content); err != nil {
			return s, err
		}
		if err := applyVerificationTransaction(&next, content.TransactionID); err != nil {
			return s, err
		}
		next.Commitment = strings.TrimSpace(content.Commitment)
		next.State = SASStateAccepted
	case EventKeyVerificationKey:
		if s.State != SASStateAccepted {
			return s, verificationTransitionError(s.State, eventType)
		}
		var content VerificationKeyContent
		if err := json.Unmarshal(raw, &content); err != nil {
			return s, err
		}
		if err := applyVerificationTransaction(&next, content.TransactionID); err != nil {
			return s, err
		}
		next.EphemeralPublicKey = strings.TrimSpace(content.Key)
		next.State = SASStateKeyReceived
	case EventKeyVerificationMAC:
		if s.State != SASStateKeyReceived {
			return s, verificationTransitionError(s.State, eventType)
		}
		var content VerificationMACContent
		if err := json.Unmarshal(raw, &content); err != nil {
			return s, err
		}
		if err := applyVerificationTransaction(&next, content.TransactionID); err != nil {
			return s, err
		}
		next.State = SASStateMACReceived
	case EventKeyVerificationDone:
		if s.State != SASStateMACReceived {
			return s, verificationTransitionError(s.State, eventType)
		}
		var content VerificationDoneContent
		if err := json.Unmarshal(raw, &content); err != nil {
			return s, err
		}
		if err := applyVerificationTransaction(&next, content.TransactionID); err != nil {
			return s, err
		}
		next.State = SASStateDone
	case EventKeyVerificationCancel:
		var content VerificationCancelContent
		if err := json.Unmarshal(raw, &content); err != nil {
			return s, err
		}
		if err := applyVerificationTransaction(&next, content.TransactionID); err != nil {
			return s, err
		}
		next.State = SASStateCancelled
	default:
		return s, errors.New("matrix verification event unsupported")
	}
	return next, nil
}

func verificationStartCanonicalMap(start VerificationStartContent) map[string]any {
	m := map[string]any{"method": start.Method}
	if v := strings.TrimSpace(start.FromDevice); v != "" {
		m["from_device"] = v
	}
	if v := strings.TrimSpace(start.TransactionID); v != "" {
		m["transaction_id"] = v
	}
	if len(start.KeyAgreementProtocols) > 0 {
		m["key_agreement_protocols"] = start.KeyAgreementProtocols
	}
	if len(start.Hashes) > 0 {
		m["hashes"] = start.Hashes
	}
	if len(start.MessageAuthenticationCodes) > 0 {
		m["message_authentication_codes"] = start.MessageAuthenticationCodes
	}
	if len(start.ShortAuthenticationString) > 0 {
		m["short_authentication_string"] = start.ShortAuthenticationString
	}
	if start.RelatesTo != nil {
		m["m.relates_to"] = start.RelatesTo
	}
	return m
}

func applyVerificationTransaction(state *SASTransactionState, transactionID string) error {
	transactionID = strings.TrimSpace(transactionID)
	if transactionID == "" {
		return nil
	}
	if state.TransactionID != "" && state.TransactionID != transactionID {
		return errors.New("matrix verification transaction mismatch")
	}
	state.TransactionID = transactionID
	return nil
}

func verificationTransitionError(state, eventType string) error {
	if state == "" {
		state = "empty"
	}
	return errors.New("matrix verification invalid transition from " + state + " on " + eventType)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func (i SASInfo) hkdfInfo() (string, error) {
	i.StartUserID = strings.TrimSpace(i.StartUserID)
	i.StartDeviceID = strings.TrimSpace(i.StartDeviceID)
	i.StartPublicKey = strings.TrimSpace(i.StartPublicKey)
	i.AcceptUserID = strings.TrimSpace(i.AcceptUserID)
	i.AcceptDeviceID = strings.TrimSpace(i.AcceptDeviceID)
	i.AcceptPublicKey = strings.TrimSpace(i.AcceptPublicKey)
	i.TransactionID = strings.TrimSpace(i.TransactionID)
	i.KeyAgreementProtocol = strings.TrimSpace(i.KeyAgreementProtocol)
	if i.KeyAgreementProtocol == "" {
		i.KeyAgreementProtocol = KeyAgreementCurve25519SHA256
	}
	switch i.KeyAgreementProtocol {
	case KeyAgreementCurve25519SHA256:
		if i.StartUserID == "" || i.StartDeviceID == "" || i.StartPublicKey == "" || i.AcceptUserID == "" || i.AcceptDeviceID == "" || i.AcceptPublicKey == "" || i.TransactionID == "" {
			return "", errors.New("matrix SAS v2 info requires users, devices, public keys, and transaction")
		}
		return "MATRIX_KEY_VERIFICATION_SAS|" + i.StartUserID + "|" + i.StartDeviceID + "|" + i.StartPublicKey + "|" + i.AcceptUserID + "|" + i.AcceptDeviceID + "|" + i.AcceptPublicKey + "|" + i.TransactionID, nil
	case KeyAgreementCurve25519:
		if i.StartUserID == "" || i.StartDeviceID == "" || i.AcceptUserID == "" || i.AcceptDeviceID == "" || i.TransactionID == "" {
			return "", errors.New("matrix SAS legacy info requires users, devices, and transaction")
		}
		return "MATRIX_KEY_VERIFICATION_SAS" + i.StartUserID + i.StartDeviceID + i.AcceptUserID + i.AcceptDeviceID + i.TransactionID, nil
	default:
		return "", errors.New("matrix SAS key agreement unsupported")
	}
}

func (i SASMACInfo) hkdfInfo(keyID string) (string, error) {
	i.UserID = strings.TrimSpace(i.UserID)
	i.DeviceID = strings.TrimSpace(i.DeviceID)
	i.OtherUserID = strings.TrimSpace(i.OtherUserID)
	i.OtherDeviceID = strings.TrimSpace(i.OtherDeviceID)
	i.TransactionID = strings.TrimSpace(i.TransactionID)
	keyID = strings.TrimSpace(keyID)
	if i.UserID == "" || i.DeviceID == "" || i.OtherUserID == "" || i.OtherDeviceID == "" || i.TransactionID == "" || keyID == "" {
		return "", errors.New("matrix SAS MAC info requires users, devices, transaction, and key id")
	}
	return "MATRIX_KEY_VERIFICATION_MAC" + i.UserID + i.DeviceID + i.OtherUserID + i.OtherDeviceID + i.TransactionID + keyID, nil
}

func sortedKeyIDs[V any](m map[string]V) []string {
	keyIDs := make([]string, 0, len(m))
	for keyID := range m {
		keyID = strings.TrimSpace(keyID)
		if keyID != "" {
			keyIDs = append(keyIDs, keyID)
		}
	}
	sort.Strings(keyIDs)
	return keyIDs
}
