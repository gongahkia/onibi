//go:build onibi_remote

package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/store"
)

const PushVAPIDSecretName = "onibi.push.vapid.v1"

type VAPIDKeys struct {
	PrivateKey string
	PublicKey  string
}

type VAPIDDiagnostics struct {
	SecretPresent        bool
	SQLitePublicPresent  bool
	PublicKeyMatches     bool
	LegacyPrivatePresent bool
	SubscriptionCount    int
}

func EnsureVAPIDKeys(context.Context, *store.DB) (VAPIDKeys, error) {
	return VAPIDKeys{}, nil
}

func RotateVAPIDKeys(context.Context, *store.DB) (VAPIDKeys, int64, error) {
	return VAPIDKeys{}, 0, errors.New("web push unavailable in remote build")
}

func VAPIDDiagnosticsForDB(context.Context, *store.DB) (VAPIDDiagnostics, error) {
	return VAPIDDiagnostics{}, errors.New("web push unavailable in remote build")
}

func (s *Server) handlePushVAPIDPublicKey(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "web push unavailable", http.StatusNotImplemented)
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "web push unavailable", http.StatusNotImplemented)
}

func SendApprovalPushNotifications(context.Context, *store.DB, *approval.Approval, *slog.Logger) {}
