package e2e

import (
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"

	"github.com/gongahkia/onibi/internal/envelope"
)

const sessionInfo = "onibi-e2e-v1"

func DeriveSessionKey(masterKey, sessionID []byte) []byte {
	if len(masterKey) != envelope.KeyBytes {
		panic(fmt.Sprintf("e2e master key must be %d bytes", envelope.KeyBytes))
	}
	if len(sessionID) == 0 {
		panic("e2e session id required")
	}
	key, err := hkdf.Key(sha256.New, masterKey, sessionID, sessionInfo, envelope.KeyBytes)
	if err != nil {
		panic(fmt.Sprintf("derive e2e session key: %v", err))
	}
	return key
}
