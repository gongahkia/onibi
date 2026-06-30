package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/gongahkia/onibi/internal/store"
)

const (
	pushVAPIDPrivateKey = "push_vapid_priv_enc"
	pushVAPIDPublicKey  = "push_vapid_pub"
)

var pushVAPIDMu sync.Mutex

type VAPIDKeys struct {
	PrivateKey string
	PublicKey  string
}

// EnsureVAPIDKeys returns the stored VAPID keypair, creating it once if absent.
func EnsureVAPIDKeys(ctx context.Context, db *store.DB) (VAPIDKeys, error) {
	if db == nil {
		return VAPIDKeys{}, errors.New("db unavailable")
	}
	pushVAPIDMu.Lock()
	defer pushVAPIDMu.Unlock()
	privateKey, privateOK, err := db.KVGetEncryptedString(ctx, pushVAPIDPrivateKey)
	if err != nil {
		return VAPIDKeys{}, err
	}
	publicKey, publicOK, err := db.KVGetString(ctx, pushVAPIDPublicKey)
	if err != nil {
		return VAPIDKeys{}, err
	}
	if privateOK && publicOK && strings.TrimSpace(privateKey) != "" && strings.TrimSpace(publicKey) != "" {
		return VAPIDKeys{PrivateKey: privateKey, PublicKey: publicKey}, nil
	}
	privateKey, publicKey, err = webpush.GenerateVAPIDKeys()
	if err != nil {
		return VAPIDKeys{}, err
	}
	if err := db.KVSetEncryptedString(ctx, pushVAPIDPrivateKey, privateKey); err != nil {
		return VAPIDKeys{}, err
	}
	if err := db.KVSetString(ctx, pushVAPIDPublicKey, publicKey); err != nil {
		return VAPIDKeys{}, err
	}
	return VAPIDKeys{PrivateKey: privateKey, PublicKey: publicKey}, nil
}

func (s *Server) handlePushVAPIDPublicKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireHTTPAuth(w, r); !ok {
		return
	}
	keys, err := EnsureVAPIDKeys(r.Context(), s.db)
	if err != nil {
		s.log.Warn("web push vapid key failed", "request_id", requestID(r), "err", err)
		http.Error(w, "push key unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"key": keys.PublicKey})
}
