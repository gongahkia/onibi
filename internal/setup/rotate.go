package setup

import (
	"context"
	"errors"
	"fmt"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

// kvKeyBotID matches auth.KVKeyBotID — we keep a copy here to avoid an
// import cycle if auth ever needs setup. Both packages agree on the string.
const kvKeyBotID = "bot_id"

// RunRotateToken prompts for a new token, validates it via getMe, ensures
// the bot id is unchanged (refuses otherwise — different bot would have
// silently severed the owner), and replaces the stored token.
func RunRotateToken(ctx context.Context, db *store.DB, sec *secrets.Store, io IO) error {
	newToken, err := promptToken(false, io)
	if err != nil {
		return err
	}

	cli, err := telegram.New(ctx, telegram.Options{Token: newToken})
	if err != nil {
		return fmt.Errorf("validate new token: %w", err)
	}
	newBotID := cli.Self.ID

	prevStr, ok, err := db.KVGetString(ctx, kvKeyBotID)
	if err != nil {
		return err
	}
	if ok && prevStr != "" {
		// quick parse — bot ids are int64; comparing via string is fine
		if prevStr != fmt.Sprintf("%d", newBotID) {
			return errors.New("token belongs to a different bot — refusing to rotate. To pair a new bot, run `onibi setup --rotate-owner`")
		}
	}
	if err := db.KVSetString(ctx, kvKeyBotID, fmt.Sprintf("%d", newBotID)); err != nil {
		return err
	}

	if err := sec.Set(secrets.KeyBotToken, newToken); err != nil {
		return err
	}
	logging.SetSecrets(newToken)

	// also touch owner — confirm it still exists; if not, prompt user
	if _, err := auth.LoadOwner(ctx, db); err != nil {
		fmt.Fprintln(io.Out, "Token rotated, but owner is not set. Run `onibi setup --rotate-owner` to pair.")
	} else {
		fmt.Fprintln(io.Out, "Token rotated. Owner unchanged.")
	}
	return nil
}
