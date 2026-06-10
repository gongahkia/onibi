package daemon

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/telegram"
)

const maxTextChunks = 5

func (d *Daemon) sendSessionPreview(ctx context.Context, api telegram.API, chatID int64, s *Session) {
	if s == nil || api == nil {
		return
	}
	header := fmt.Sprintf("[%s] preview", s.Name)
	buf := s.Buf.Snapshot()
	if render.ResolveMode(buf, d.renderOverride(s.ID)) == render.ModePNG {
		img, err := render.RenderPNG(buf, render.PNGOptions{})
		if err == nil {
			sent, sendErr := api.SendPhoto(ctx, &tgbot.SendPhotoParams{
				ChatID:  chatID,
				Caption: trimCaption(header),
				Photo: &models.InputFileUpload{
					Filename: "onibi-" + s.ID + ".png",
					Data:     bytes.NewReader(img),
				},
			})
			if sendErr == nil {
				d.bindMessage(sent, s.ID)
				return
			}
		}
	}
	body := render.TextTailBody(buf, render.Options{})
	sent, _ := d.sendTextOutput(ctx, api, chatID, header, body, "onibi-"+s.ID+".txt")
	d.bindMessage(sent, s.ID)
}

func (d *Daemon) sendTextOutput(ctx context.Context, api telegram.API, chatID int64, header, body, filename string) (*models.Message, error) {
	if api == nil {
		return nil, nil
	}
	chunks := telegram.SplitForTelegram(body, telegram.SafeTextLimit)
	if len(chunks) <= maxTextChunks {
		var last *models.Message
		for i, chunk := range chunks {
			label := header
			if len(chunks) > 1 {
				label = fmt.Sprintf("%s (%d/%d)", header, i+1, len(chunks))
			}
			sent, err := api.SendMessage(ctx, &tgbot.SendMessageParams{
				ChatID:    chatID,
				Text:      telegram.HTMLCode(label, chunk),
				ParseMode: models.ParseModeHTML,
			})
			if err != nil {
				return last, err
			}
			last = sent
		}
		return last, nil
	}
	if filename == "" {
		filename = "onibi-output.txt"
	}
	return api.SendDocument(ctx, &tgbot.SendDocumentParams{
		ChatID:  chatID,
		Caption: trimCaption(header),
		Document: &models.InputFileUpload{
			Filename: filename,
			Data:     strings.NewReader(body),
		},
	})
}
