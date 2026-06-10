package telegram

import (
	"strings"

	"github.com/go-telegram/bot/models"
)

// Callback data prefixes. Telegram limits callback_data to 64 bytes; our
// ids are 16 hex chars so verb:id stays well under.
const (
	CBApprove = "approve:"
	CBDeny    = "deny:"
	CBEdit    = "edit:"
	CBTarget  = "target:"
	CBPromptSend   = "psend:"
	CBPromptEdit   = "pedit:"
	CBPromptCancel = "pcancel:"
	CBPromptUp     = "pup:"
	CBPromptDown   = "pdown:"
	CBPeek         = "peek:"
	CBInterrupt    = "int:"
	CBKill         = "kill:"
)

type SessionTarget struct {
	ID    string
	Label string
}

// ApprovalKeyboard returns the inline keyboard rendered alongside an
// approval request. Three buttons in one row so they stay tappable on a
// phone without horizontal scroll.
func ApprovalKeyboard(approvalID string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "Approve", CallbackData: CBApprove + approvalID},
				{Text: "Deny", CallbackData: CBDeny + approvalID},
				{Text: "Edit", CallbackData: CBEdit + approvalID},
			},
		},
	}
}

// DecidedKeyboard replaces the approval keyboard after a decision lands,
// leaving a single non-interactive label so the user can see at a glance
// what state the row ended in.
func DecidedKeyboard(label string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: label, CallbackData: "noop"}},
		},
	}
}

func SessionTargetKeyboard(targets []SessionTarget) *models.InlineKeyboardMarkup {
	rows := make([][]models.InlineKeyboardButton, 0, len(targets))
	for _, t := range targets {
		label := t.Label
		if label == "" {
			label = t.ID
		}
		rows = append(rows, []models.InlineKeyboardButton{{
			Text:         trimButton(label),
			CallbackData: CBTarget + t.ID,
		}})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func PromptKeyboard(id string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "Send now", CallbackData: CBPromptSend + id},
				{Text: "Edit", CallbackData: CBPromptEdit + id},
				{Text: "Cancel", CallbackData: CBPromptCancel + id},
			},
			{
				{Text: "Up", CallbackData: CBPromptUp + id},
				{Text: "Down", CallbackData: CBPromptDown + id},
			},
		},
	}
}

func SessionMenuKeyboard(targets []SessionTarget) *models.InlineKeyboardMarkup {
	rows := make([][]models.InlineKeyboardButton, 0, len(targets)*2)
	for _, t := range targets {
		label := t.Label
		if label == "" {
			label = t.ID
		}
		rows = append(rows, []models.InlineKeyboardButton{{Text: trimButton(label), CallbackData: CBTarget + t.ID}})
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "Peek", CallbackData: CBPeek + t.ID},
			{Text: "Interrupt", CallbackData: CBInterrupt + t.ID},
			{Text: "Kill", CallbackData: CBKill + t.ID},
		})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// ParseCallback splits a callback_data string into (verb, approvalID).
// Returns ("", "") for unknown verbs so callers can ignore.
func ParseCallback(data string) (verb, id string) {
	switch {
	case strings.HasPrefix(data, CBApprove):
		return "approve", strings.TrimPrefix(data, CBApprove)
	case strings.HasPrefix(data, CBDeny):
		return "deny", strings.TrimPrefix(data, CBDeny)
	case strings.HasPrefix(data, CBEdit):
		return "edit", strings.TrimPrefix(data, CBEdit)
	case strings.HasPrefix(data, CBTarget):
		return "target", strings.TrimPrefix(data, CBTarget)
	case strings.HasPrefix(data, CBPromptSend):
		return "prompt_send", strings.TrimPrefix(data, CBPromptSend)
	case strings.HasPrefix(data, CBPromptEdit):
		return "prompt_edit", strings.TrimPrefix(data, CBPromptEdit)
	case strings.HasPrefix(data, CBPromptCancel):
		return "prompt_cancel", strings.TrimPrefix(data, CBPromptCancel)
	case strings.HasPrefix(data, CBPromptUp):
		return "prompt_up", strings.TrimPrefix(data, CBPromptUp)
	case strings.HasPrefix(data, CBPromptDown):
		return "prompt_down", strings.TrimPrefix(data, CBPromptDown)
	case strings.HasPrefix(data, CBPeek):
		return "peek", strings.TrimPrefix(data, CBPeek)
	case strings.HasPrefix(data, CBInterrupt):
		return "interrupt", strings.TrimPrefix(data, CBInterrupt)
	case strings.HasPrefix(data, CBKill):
		return "kill", strings.TrimPrefix(data, CBKill)
	}
	return "", ""
}

func trimButton(s string) string {
	r := []rune(s)
	if len(r) <= 40 {
		return s
	}
	return string(r[:37]) + "..."
}
