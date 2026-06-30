package e2e

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

type vectorFile struct {
	Version string   `json:"version"`
	KDF     string   `json:"kdf"`
	Info    string   `json:"info"`
	AEAD    string   `json:"aead"`
	Vectors []vector `json:"vectors"`
}

type vector struct {
	Name          string `json:"name"`
	MasterKeyHex  string `json:"masterKeyHex"`
	SessionID     string `json:"sessionID"`
	IVHex         string `json:"ivHex"`
	PlaintextHex  string `json:"plaintextHex"`
	AADHex        string `json:"aadHex"`
	SessionKeyHex string `json:"sessionKeyHex"`
	CiphertextHex string `json:"ciphertextHex"`
}

func TestEnvelopeVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var file vectorFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if file.Version != "onibi.e2e.vectors.v1" || file.KDF != "HKDF-SHA256" || file.Info != sessionInfo || file.AEAD != "AES-256-GCM" {
		t.Fatalf("bad vector metadata: %+v", file)
	}
	for _, tc := range file.Vectors {
		t.Run(tc.Name, func(t *testing.T) {
			master := mustDecodeHex(t, tc.MasterKeyHex)
			iv := mustDecodeHex(t, tc.IVHex)
			plain := mustDecodeHex(t, tc.PlaintextHex)
			aad := mustDecodeHex(t, tc.AADHex)
			wantKey := mustDecodeHex(t, tc.SessionKeyHex)
			wantCiphertext := mustDecodeHex(t, tc.CiphertextHex)

			key := DeriveSessionKey(master, []byte(tc.SessionID))
			if !bytes.Equal(key, wantKey) {
				t.Fatalf("session key = %x, want %x", key, wantKey)
			}
			block, err := aes.NewCipher(key)
			if err != nil {
				t.Fatal(err)
			}
			gcm, err := cipher.NewGCM(block)
			if err != nil {
				t.Fatal(err)
			}
			gotCiphertext := gcm.Seal(nil, iv, plain, aad)
			if !bytes.Equal(gotCiphertext, wantCiphertext) {
				t.Fatalf("ciphertext = %x, want %x", gotCiphertext, wantCiphertext)
			}
			gotPlain, err := gcm.Open(nil, iv, gotCiphertext, aad)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotPlain, plain) {
				t.Fatalf("plaintext = %x, want %x", gotPlain, plain)
			}
		})
	}
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	out, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
