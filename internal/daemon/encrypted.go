package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/telegram"
)

const (
	encryptedPayloadTTL = 24 * time.Hour
	secureActionTTL     = 5 * time.Minute
	maxMiniAppURLLen    = 1800
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

func (d *Daemon) encryptedModeStrict() bool {
	return strings.EqualFold(strings.TrimSpace(d.EncryptedMode), "on")
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
	ctxPayload := secureContext{Agents: supportedAgentNames()}
	for _, s := range d.liveSessions() {
		ctxPayload.Sessions = append(ctxPayload.Sessions, secureSession{
			ID:    s.ID,
			Label: fmt.Sprintf("%s %s %s", s.Name, s.Agent, s.ID),
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
		Text:        "Encrypted Onibi controls. Open Mini App.",
		ReplyMarkup: telegram.SecureComposerKeyboard(url),
	})
}

func (d *Daemon) sendSecureRequired(ctx context.Context, api telegram.API, chatID int64) {
	if _, err := d.sendSecureComposer(ctx, api, chatID); err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Encrypted mode unavailable. Run onibi setup --enable-encrypted-mode."})
	}
}
