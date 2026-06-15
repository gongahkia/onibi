package telegram

import (
	"html"
	"strings"
	"unicode/utf8"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const SafeTextLimit = 3500

func EscapeHTML(s string) string {
	return html.EscapeString(s)
}

func HTMLPre(body string) string {
	return "<pre>" + html.EscapeString(body) + "</pre>"
}

func HTMLCode(label, body string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return HTMLPre(body)
	}
	return html.EscapeString(label) + "\n" + HTMLPre(body)
}

func NormalizeSendMessageParams(params *tgbot.SendMessageParams) *tgbot.SendMessageParams {
	if params == nil {
		return nil
	}
	cp := *params
	if cp.ParseMode == "" && len(cp.Entities) == 0 && cp.Text != "" {
		cp.Text = html.EscapeString(cp.Text)
		cp.ParseMode = models.ParseModeHTML
	}
	return &cp
}

func NormalizeSendPhotoParams(params *tgbot.SendPhotoParams) *tgbot.SendPhotoParams {
	if params == nil {
		return nil
	}
	cp := *params
	if cp.ParseMode == "" && len(cp.CaptionEntities) == 0 && cp.Caption != "" {
		cp.Caption = html.EscapeString(cp.Caption)
		cp.ParseMode = models.ParseModeHTML
	}
	return &cp
}

func NormalizeSendDocumentParams(params *tgbot.SendDocumentParams) *tgbot.SendDocumentParams {
	if params == nil {
		return nil
	}
	cp := *params
	if cp.ParseMode == "" && len(cp.CaptionEntities) == 0 && cp.Caption != "" {
		cp.Caption = html.EscapeString(cp.Caption)
		cp.ParseMode = models.ParseModeHTML
	}
	return &cp
}

func NormalizeEditMessageTextParams(params *tgbot.EditMessageTextParams) *tgbot.EditMessageTextParams {
	if params == nil {
		return nil
	}
	cp := *params
	if cp.ParseMode == "" && len(cp.Entities) == 0 && cp.Text != "" {
		cp.Text = html.EscapeString(cp.Text)
		cp.ParseMode = models.ParseModeHTML
	}
	return &cp
}

func SplitForTelegram(s string, limit int) []string {
	if limit <= 0 {
		limit = SafeTextLimit
	}
	if len(s) <= limit {
		return []string{s}
	}
	var out []string
	for len(s) > limit {
		cut := limit
		if i := strings.LastIndexByte(s[:limit], '\n'); i > limit/2 {
			cut = i + 1
		} else {
			for !utf8.RuneStart(s[cut]) && cut > 0 {
				cut--
			}
		}
		out = append(out, s[:cut])
		s = s[cut:]
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}
