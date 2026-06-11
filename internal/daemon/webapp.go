package daemon

import (
	"context"
	"encoding/json"
	"errors"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/telegram"
)

type webAppDecision struct {
	Version      int    `json:"v"`
	Action       string `json:"action"`
	ID           string `json:"id"`
	UpdatedInput string `json:"updated_input,omitempty"`
}

func (d *Daemon) onWebAppData(ctx context.Context, api telegram.API, m *models.Message) error {
	var dec webAppDecision
	if err := json.Unmarshal([]byte(m.WebAppData.Data), &dec); err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Invalid Mini App payload."})
		return nil
	}
	if dec.Version != 1 || dec.ID == "" {
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
		editJSON, authErr := d.prepareApprovalEdit(ctx, dec.UpdatedInput)
		if authErr != "" {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: authErr})
			return nil
		}
		if err := approval.ValidateEditedInput(a.Tool, a.InputJSON, editJSON); err != nil {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Invalid edited input: " + err.Error()})
			return nil
		}
		res, err := d.Queue.DecideWithResult(ctx, a.ID, approval.VerdictEdit, editJSON, "", m.From.ID)
		return d.finishWebAppDecision(ctx, api, m.Chat.ID, a, res, err, "Edited input accepted.")
	default:
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Unknown Mini App action."})
		return nil
	}
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
