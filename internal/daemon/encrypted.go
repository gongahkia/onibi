package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/miniappurl"
	"github.com/gongahkia/onibi/internal/telegram"
)

const (
	encryptedPayloadTTL = 24 * time.Hour
	secureActionTTL     = 5 * time.Minute
	maxMiniAppURLLen    = 1800
	secureLastActionKey = "secure:last_webapp_action"
)

type secureSession struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type secureContext struct {
	Sessions []secureSession `json:"sessions"`
	Agents   []string        `json:"agents"`
}

func (d *Daemon) encryptedModeEnabled() bool {
	mode := strings.ToLower(strings.TrimSpace(d.EncryptedMode))
	return mode == "on" || mode == "ask"
}

func (d *Daemon) sendEncryptedText(ctx context.Context, api telegram.API, chatID int64, kind, title, body string) (*models.Message, error) {
	return d.sendEncryptedPlain(ctx, api, chatID, envelope.Plain{
		Kind:  kind,
		Title: title,
		Body:  body,
	}, "onibi-"+kind+".enc")
}

func (d *Daemon) sendMaybeEncryptedText(ctx context.Context, api telegram.API, chatID int64, kind, title, body string) (*models.Message, error) {
	if !d.encryptedModeEnabled() {
		if api == nil {
			return nil, nil
		}
		return api.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: body})
	}
	sent, err := d.sendEncryptedText(ctx, api, chatID, kind, title, body)
	if err != nil {
		d.sendSecureRequired(ctx, api, chatID)
	}
	return sent, err
}

func (d *Daemon) sendEncryptedImage(ctx context.Context, api telegram.API, chatID int64, title string, data []byte, filename string) (*models.Message, error) {
	if filename == "" {
		filename = "onibi.png"
	}
	return d.sendEncryptedPlain(ctx, api, chatID, envelope.Plain{
		Kind:    "image",
		Title:   title,
		Body:    title,
		MIME:    "image/png",
		DataB64: base64.StdEncoding.EncodeToString(data),
		File:    filename,
	}, strings.TrimSuffix(filename, ".png")+".enc")
}

func (d *Daemon) sendEncryptedPlain(ctx context.Context, api telegram.API, chatID int64, plain envelope.Plain, fallbackFilename string) (*models.Message, error) {
	if api == nil {
		return nil, nil
	}
	if strings.TrimSpace(d.EnvelopeSeed) == "" {
		return nil, errors.New("encrypted mode enabled without envelope seed; run `onibi setup --enable-encrypted-mode`")
	}
	token, err := envelope.Encrypt(d.EnvelopeSeed, plain, time.Now().Add(encryptedPayloadTTL))
	if err != nil {
		return nil, err
	}
	url, err := envelope.BuildMiniAppURL(d.MiniAppURL, token)
	if err != nil {
		return nil, err
	}
	if len(url) <= maxMiniAppURLLen {
		return api.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID:      chatID,
			Text:        "Encrypted Onibi item. Open Mini App to decrypt.",
			ReplyMarkup: telegram.EncryptedItemKeyboard(url),
		})
	}
	openURL, err := envelope.BuildOpenURL(d.MiniAppURL)
	if err != nil {
		return nil, err
	}
	if fallbackFilename == "" {
		fallbackFilename = "onibi.enc"
	}
	return api.SendDocument(ctx, &tgbot.SendDocumentParams{
		ChatID:  chatID,
		Caption: "Encrypted Onibi item. Open Mini App and import ciphertext.",
		Document: &models.InputFileUpload{
			Filename: fallbackFilename,
			Data:     strings.NewReader(token),
		},
		ReplyMarkup: telegram.EncryptedItemKeyboard(openURL),
	})
}

func (d *Daemon) sendSecureComposer(ctx context.Context, api telegram.API, chatID int64) (*models.Message, error) {
	return d.sendSecureComposerText(ctx, api, chatID, "Encrypted Onibi controls. Open Mini App.")
}

func (d *Daemon) sendSecureComposerText(ctx context.Context, api telegram.API, chatID int64, text string) (*models.Message, error) {
	ctxPayload := secureContext{Agents: supportedAgentNames()}
	for _, c := range d.sessionCards(ctx, chatID, d.liveSessions()) {
		ctxPayload.Sessions = append(ctxPayload.Sessions, secureSession{
			ID:    c.ID,
			Label: c.Label(),
		})
	}
	b, err := json.Marshal(ctxPayload)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(d.EnvelopeSeed) == "" {
		return nil, errors.New("encrypted mode enabled without envelope seed; run `onibi setup --enable-encrypted-mode`")
	}
	token, err := envelope.Encrypt(d.EnvelopeSeed, envelope.Plain{
		Kind:  "secure",
		Title: "Secure controls",
		Body:  string(b),
	}, time.Now().Add(encryptedPayloadTTL))
	if err != nil {
		return nil, err
	}
	url, err := envelope.BuildMiniAppURL(d.MiniAppURL, token)
	if err != nil {
		return nil, err
	}
	if len(url) > maxMiniAppURLLen {
		return d.sendEncryptedText(ctx, api, chatID, "secure", "Secure controls", string(b))
	}
	return api.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: telegram.SecureComposerKeyboard(url),
	})
}

func (d *Daemon) sendSecureRequired(ctx context.Context, api telegram.API, chatID int64) {
	if _, err := d.sendSecureComposer(ctx, api, chatID); err != nil {
		d.sendSecureUnavailable(ctx, api, chatID)
	}
}

func (d *Daemon) sendSecureBlocked(ctx context.Context, api telegram.API, chatID int64) {
	if _, err := d.sendSecureComposerText(ctx, api, chatID, "Plaintext command blocked in encrypted mode. Open secure controls."); err != nil {
		d.sendSecureUnavailable(ctx, api, chatID)
	}
}

func (d *Daemon) sendSecureUnavailable(ctx context.Context, api telegram.API, chatID int64) {
	sendMessage(ctx, api, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   "Encrypted mode unavailable.\n\n" + d.secureStatusText(ctx) + "\n\nRun onibi setup --enable-encrypted-mode.",
	})
}

type secureReadiness struct {
	mode                  string
	seedPresent           bool
	miniAppURLSet         bool
	miniAppURLAllowed     bool
	webAppLastSeen        time.Time
	webAppLastSeenPresent bool
	plaintextBlocked      bool
	secureButtonAvailable bool
}

func (d *Daemon) secureReadiness(ctx context.Context) secureReadiness {
	r := secureReadiness{
		mode:              d.encryptedModeLabel(),
		seedPresent:       strings.TrimSpace(d.EnvelopeSeed) != "",
		miniAppURLSet:     strings.TrimSpace(d.MiniAppURL) != "",
		miniAppURLAllowed: miniappurl.Allowed(d.MiniAppURL),
		plaintextBlocked:  d.encryptedModeEnabled(),
	}
	r.secureButtonAvailable = r.seedPresent && r.miniAppURLAllowed
	if d.DB == nil {
		return r
	}
	v, ok, err := d.DB.KVGetString(ctx, secureLastActionKey)
	if err != nil || !ok {
		return r
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n <= 0 {
		return r
	}
	r.webAppLastSeen = time.Unix(n, 0)
	r.webAppLastSeenPresent = true
	return r
}

func (d *Daemon) secureStatus(ctx context.Context) string {
	r := d.secureReadiness(ctx)
	return fmt.Sprintf("%s, seed %s, mini app %s, url %s, devices paired %s, webapp %s, plaintext %s, /secure %s",
		r.mode,
		okMissing(r.seedPresent),
		okMissing(r.miniAppURLSet),
		miniAppURLStatus(r),
		devicesPairedLabel(r),
		webAppLastSeenLabel(r),
		blockedAllowed(r.plaintextBlocked),
		okMissing(r.secureButtonAvailable),
	)
}

func (d *Daemon) secureStatusText(ctx context.Context) string {
	r := d.secureReadiness(ctx)
	lines := []string{
		"Secure mode readiness",
		"mode=" + r.mode,
		"seed_present=" + yesNo(r.seedPresent),
		"devices_paired=" + devicesPairedLabel(r),
		"mini_app_url_set=" + yesNo(r.miniAppURLSet),
		"mini_app_url_allowed=" + yesNo(r.miniAppURLAllowed),
		"webapp_action_last_seen=" + webAppLastSeenLabel(r),
		"plaintext_commands_blocked=" + yesNo(r.plaintextBlocked),
		"secure_button_available=" + yesNo(r.secureButtonAvailable),
	}
	return strings.Join(lines, "\n")
}

func okMissing(ok bool) string {
	if ok {
		return "ok"
	}
	return "missing"
}

func yesNo(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}

func miniAppURLStatus(r secureReadiness) string {
	if r.miniAppURLAllowed {
		return "ok"
	}
	if r.miniAppURLSet {
		return "invalid"
	}
	return "missing"
}

func blockedAllowed(blocked bool) string {
	if blocked {
		return "blocked"
	}
	return "allowed"
}

func devicesPairedLabel(r secureReadiness) string {
	if r.webAppLastSeenPresent {
		return "observed"
	}
	return "unknown"
}

func webAppLastSeenLabel(r secureReadiness) string {
	if !r.webAppLastSeenPresent {
		return "never"
	}
	age := time.Since(r.webAppLastSeen)
	if age < 0 {
		return "now"
	}
	return age.Truncate(time.Second).String() + " ago"
}

func (d *Daemon) recordSecureWebAppAction(ctx context.Context) {
	if d.DB == nil {
		return
	}
	_ = d.DB.KVSetString(ctx, secureLastActionKey, strconv.FormatInt(time.Now().Unix(), 10))
}
