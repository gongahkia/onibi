package matrix

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
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
