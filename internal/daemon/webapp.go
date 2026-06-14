package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/telegram"
)

type webAppDecision struct {
	Version      int    `json:"v"`
	Action       string `json:"action"`
	ID           string `json:"id"`
	UpdatedInput string `json:"updated_input,omitempty"`
	Session      string `json:"session,omitempty"`
	Text         string `json:"text,omitempty"`
	Agent        string `json:"agent,omitempty"`
	Args         string `json:"args,omitempty"`
}

type webAppEnvelopePayload struct {
	Version  int    `json:"v"`
	Envelope string `json:"envelope"`
}

func (d *Daemon) onWebAppData(ctx context.Context, api telegram.API, m *models.Message) error {
	dec, err := d.parseWebAppDecision(m.WebAppData.Data)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Invalid Mini App payload."})
		return nil
	}
	if dec.Version != 1 {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Invalid Mini App payload."})
		return nil
	}
	switch dec.Action {
	case "prompt":
		if strings.TrimSpace(dec.Text) == "" {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Prompt is empty."})
			return nil
		}
		return d.enqueuePromptText(ctx, api, m.Chat.ID, dec.Session, dec.Text)
	case "interrupt":
		d.handleInterruptCommand(ctx, api, m.Chat.ID, dec.Session)
		return nil
	case "kill":
		d.handleKillCommand(ctx, api, m.Chat.ID, dec.Session)
		return nil
	case "target":
		d.handleTargetCommand(ctx, api, m.Chat.ID, dec.Session)
		return nil
	}
	if dec.ID == "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Invalid Mini App payload."})
		return nil
	}
	a, err := d.Queue.Get(ctx, dec.ID)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Unknown approval.", ReplyMarkup: removeKeyboard()})
		return nil
	}
	if a.State != approval.StatePending {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Approval already decided.", ReplyMarkup: removeKeyboard()})
		return nil
	}
	switch dec.Action {
	case "approve":
		if isHighRiskApproval(a) {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "High-risk approval requires the confirm approve action in the Mini App."})
			return nil
		}
		res, err := d.Queue.DecideWithResult(ctx, a.ID, approval.VerdictApprove, "", "", m.From.ID)
		return d.finishWebAppDecision(ctx, api, m.Chat.ID, a, res, err, "Approved.")
	case "confirm_approve":
		res, err := d.Queue.DecideWithResult(ctx, a.ID, approval.VerdictApprove, "", "", m.From.ID)
		return d.finishWebAppDecision(ctx, api, m.Chat.ID, a, res, err, "Approved.")
	case "deny":
		res, err := d.Queue.DecideWithResult(ctx, a.ID, approval.VerdictDeny, "", "denied by owner via encrypted Mini App", m.From.ID)
		return d.finishWebAppDecision(ctx, api, m.Chat.ID, a, res, err, "Denied.")
	case "edit":
		editJSON, authErr, authNote := d.prepareApprovalEdit(ctx, m.Chat.ID, dec.UpdatedInput)
		if authErr != "" {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: authErr})
			return nil
		}
		if err := approval.ValidateEditedInput(a.Tool, a.InputJSON, editJSON); err != nil {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Invalid edited input: " + err.Error()})
			return nil
		}
		res, err := d.Queue.DecideWithResult(ctx, a.ID, approval.VerdictEdit, editJSON, "", m.From.ID)
		return d.finishWebAppDecision(ctx, api, m.Chat.ID, a, res, err, withTOTPNote("Edited input accepted.", authNote))
	default:
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Unknown Mini App action."})
		return nil
	}
}

func (d *Daemon) parseWebAppDecision(data string) (webAppDecision, error) {
	var wrapped webAppEnvelopePayload
	if err := json.Unmarshal([]byte(data), &wrapped); err == nil && wrapped.Envelope != "" {
		if strings.TrimSpace(d.EnvelopeSeed) == "" {
			return webAppDecision{}, errors.New("missing envelope seed")
		}
		plain, err := envelope.Decrypt(d.EnvelopeSeed, wrapped.Envelope, time.Now())
		if err != nil {
			return webAppDecision{}, err
		}
		if plain.Kind != "action" {
			return webAppDecision{}, errors.New("invalid envelope kind")
		}
		var dec webAppDecision
		if err := json.Unmarshal([]byte(plain.Body), &dec); err != nil {
			return webAppDecision{}, err
		}
		return dec, nil
	}
	if d.encryptedModeEnabled() {
		return webAppDecision{}, errors.New("plaintext Mini App data rejected")
	}
	var dec webAppDecision
	if err := json.Unmarshal([]byte(data), &dec); err != nil {
		return webAppDecision{}, err
	}
	return dec, nil
}

func (d *Daemon) finishWebAppDecision(ctx context.Context, api telegram.API, chatID int64, a *approval.Approval, res approval.DecisionResult, err error, okText string) error {
	if errors.Is(err, approval.ErrExpired) {
		if !res.Delivered {
			d.editStoredDecision(ctx, a, res.Decision)
		}
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Approval expired.", ReplyMarkup: removeKeyboard()})
		return nil
	}
	if errors.Is(err, approval.ErrAlreadyDecided) {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Approval already decided.", ReplyMarkup: removeKeyboard()})
		return nil
	}
	if err != nil {
		return err
	}
	if !res.Delivered {
		d.editStoredDecision(ctx, a, res.Decision)
	}
	sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: okText, ReplyMarkup: removeKeyboard()})
	return nil
}

func isHighRiskApproval(a *approval.Approval) bool {
	if a == nil {
		return false
	}
	return approval.ClassifyRisk(a.Tool, a.InputJSON).Level == "high"
}

func removeKeyboard() *models.ReplyKeyboardRemove {
	return &models.ReplyKeyboardRemove{RemoveKeyboard: true}
}
