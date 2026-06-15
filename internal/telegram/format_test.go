package telegram

import (
	"testing"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func TestNormalizeSendMessageParamsEscapesPlainHTML(t *testing.T) {
	in := &tgbot.SendMessageParams{ChatID: int64(1), Text: "Usage: /target <id|name> & ok"}
	got := NormalizeSendMessageParams(in)
	if got.Text != "Usage: /target &lt;id|name&gt; &amp; ok" {
		t.Fatalf("text = %q", got.Text)
	}
	if got.ParseMode != models.ParseModeHTML {
		t.Fatalf("parse mode = %q", got.ParseMode)
	}
	if in.Text != "Usage: /target <id|name> & ok" {
		t.Fatalf("input mutated: %q", in.Text)
	}
}

func TestNormalizeSendMessageParamsPreservesExplicitParseMode(t *testing.T) {
	in := &tgbot.SendMessageParams{ChatID: int64(1), Text: "<b>ok</b>", ParseMode: models.ParseModeHTML}
	got := NormalizeSendMessageParams(in)
	if got.Text != "<b>ok</b>" || got.ParseMode != models.ParseModeHTML {
		t.Fatalf("params = %#v", got)
	}
}

func TestNormalizeSendMessageParamsPreservesEntities(t *testing.T) {
	in := &tgbot.SendMessageParams{
		ChatID:   int64(1),
		Text:     "<raw>",
		Entities: []models.MessageEntity{{Type: "code", Offset: 0, Length: 5}},
	}
	got := NormalizeSendMessageParams(in)
	if got.Text != "<raw>" || got.ParseMode != "" {
		t.Fatalf("params = %#v", got)
	}
}

func TestNormalizeCaptionsEscapesPlainHTML(t *testing.T) {
	photo := NormalizeSendPhotoParams(&tgbot.SendPhotoParams{ChatID: int64(1), Caption: "[x] <preview>"})
	if photo.Caption != "[x] &lt;preview&gt;" || photo.ParseMode != models.ParseModeHTML {
		t.Fatalf("photo = %#v", photo)
	}
	doc := NormalizeSendDocumentParams(&tgbot.SendDocumentParams{ChatID: int64(1), Caption: "log & tail"})
	if doc.Caption != "log &amp; tail" || doc.ParseMode != models.ParseModeHTML {
		t.Fatalf("doc = %#v", doc)
	}
}

func TestNormalizeEditMessageTextParamsEscapesPlainHTML(t *testing.T) {
	got := NormalizeEditMessageTextParams(&tgbot.EditMessageTextParams{ChatID: int64(1), MessageID: 2, Text: "Use /new <agent>"})
	if got.Text != "Use /new &lt;agent&gt;" || got.ParseMode != models.ParseModeHTML {
		t.Fatalf("edit = %#v", got)
	}
}
