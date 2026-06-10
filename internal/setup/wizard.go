package setup

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

type pairClient interface {
	telegram.API
}

var newPairClient = func(ctx context.Context, token string, handler telegram.HandlerFunc) (pairClient, error) {
	return telegram.New(ctx, telegram.Options{
		Token:      token,
		APIHandler: handler,
	})
}

// Flags configures wizard behavior.
type Flags struct {
	// RotateOwner allows overwriting an existing owner.
	RotateOwner bool
	// EnableTOTP generates and stores a TOTP secret; mints a TOTP QR.
	EnableTOTP bool
	// Paranoid implies EnableTOTP plus tighter approval expiry (60s) and
	// confirm-tap on presets. The flag is captured here; consumers (the
	// daemon) read it from KV.
	Paranoid bool
	// TokenStdin reads the bot token from os.Stdin instead of prompting.
	TokenStdin bool
	// PairTimeout overrides the default time to wait for the user to tap
	// the deeplink before we give up.
	PairTimeout time.Duration
}

// IO holds the streams the wizard talks to. Allows tests to drive the
// wizard with strings.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Std returns an IO bound to stdin/stdout/stderr.
func Std() IO { return IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr} }

// Run executes the interactive pair-once setup flow:
//  1. token paste + getMe validation + Keychain (or .env) store
//  2. owner-not-already-set check (or --rotate-owner)
//  3. mint single-use pairing token, print deeplink + QR
//  4. listen for /start pair_<token> on the bot, atomically consume,
//     persist owner_id
//  5. mandatory Telegram 2FA acknowledgment gate
//
// Returns the owner chat ID on success.
func Run(ctx context.Context, db *store.DB, sec *secrets.Store, flags Flags, io IO) (int64, error) {
	if flags.PairTimeout <= 0 {
		flags.PairTimeout = 10 * time.Minute
	}

	// owner sanity check up front
	if !flags.RotateOwner {
		exists, err := auth.IsOwnerSet(ctx, db)
		if err != nil {
			return 0, fmt.Errorf("check owner: %w", err)
		}
		if exists {
			return 0, errors.New("owner already paired — re-run with --rotate-owner to replace, or `onibi rotate-token` to keep owner and rotate token")
		}
	}

	// Step A: token
	token, err := promptToken(flags.TokenStdin, io)
	if err != nil {
		return 0, err
	}
	if err := sec.Set(secrets.KeyBotToken, token); err != nil {
		return 0, fmt.Errorf("store bot token: %w", err)
	}
	// register for log-scrubbing immediately
	logging.SetSecrets(token)

	// Step B: getMe + mint pairing token + print deeplink + QR
	pairCtx, pairCancel := context.WithTimeout(ctx, flags.PairTimeout)
	defer pairCancel()

	chatID, err := pairOnce(pairCtx, db, token, io)
	if err != nil {
		return 0, err
	}

	o := &auth.Owner{}
	if err := auth.SetOwner(ctx, db, o, chatID); err != nil {
		return 0, fmt.Errorf("persist owner: %w", err)
	}
	fmt.Fprintf(io.Out, "\nPaired — owner chat id %d recorded permanently.\n", chatID)

	// Step C: optional TOTP
	if flags.EnableTOTP || flags.Paranoid {
		if err := enableTOTP(ctx, sec, io); err != nil {
			return 0, fmt.Errorf("enable totp: %w", err)
		}
	}
	if flags.Paranoid {
		if err := db.KVSetString(ctx, "paranoid", "1"); err != nil {
			return 0, err
		}
		fmt.Fprintln(io.Out, "Paranoid mode enabled: approval expiry 60s, confirm-tap on presets.")
	}

	// Step C': Telegram 2FA acknowledgment (mandatory)
	if err := acknowledge2FA(ctx, db, io); err != nil {
		return 0, err
	}

	fmt.Fprintln(io.Out, "\nSetup complete. Next steps:")
	fmt.Fprintln(io.Out, "  • onibi install-service     # auto-start on every login")
	fmt.Fprintln(io.Out, "  • onibi install-hooks       # wire up coding-agent hooks")
	fmt.Fprintln(io.Out, "  • onibi doctor              # health + integrity check")
	fmt.Fprintln(io.Out, "  • onibi run                 # start the daemon manually")
	return chatID, nil
}

// promptToken reads the bot token either from stdin (--token-stdin) or
// from an interactive prompt. Never echoed back to stdout.
func promptToken(fromStdin bool, io IO) (string, error) {
	if fromStdin {
		b, err := readAllTrimmed(io.In)
		if err != nil {
			return "", err
		}
		if b == "" {
			return "", errors.New("empty token on stdin")
		}
		return b, nil
	}
	suggested := suggestUsername()
	fmt.Fprintln(io.Out, "Onibi setup — pair once, then linked.")
	fmt.Fprintln(io.Out, "")
	fmt.Fprintln(io.Out, "1) On your phone, open Telegram → @BotFather → /newbot.")
	fmt.Fprintln(io.Out, "   Pick a display name and an unguessable username.")
	fmt.Fprintf(io.Out, "   Suggested username: %s\n", suggested)
	fmt.Fprintln(io.Out, "   Copy the token BotFather returns (looks like 1234567890:AA...).")
	fmt.Fprintln(io.Out, "")
	fmt.Fprint(io.Out, "Paste token here: ")
	br := bufio.NewReader(io.In)
	line, err := br.ReadString('\n')
	if err != nil && err != io2EOF() {
		return "", err
	}
	token := strings.TrimSpace(line)
	if token == "" {
		return "", errors.New("empty token")
	}
	return token, nil
}

// pairOnce sets up a Telegram client, mints the pairing token, prints the
// deeplink + terminal QR, then long-polls for the matching /start. Returns
// the owner chat id on success.
func pairOnce(ctx context.Context, db *store.DB, token string, io IO) (int64, error) {
	doneCh := make(chan int64, 1)
	errCh := make(chan error, 1)

	pairTok, err := NewToken(ctx, db)
	if err != nil {
		return 0, err
	}

	handler := func(ctx context.Context, api telegram.API, update *models.Update) {
		// only act on /start messages with our prefix; ignore everything
		// else during pair (owner isn't established yet — we have no
		// other auth signal to trust)
		if update.Message == nil || update.Message.From == nil {
			return
		}
		text := strings.TrimSpace(update.Message.Text)
		const startCmd = "/start "
		if !strings.HasPrefix(text, startCmd) {
			return
		}
		payload := strings.TrimSpace(strings.TrimPrefix(text, startCmd))
		got, ok := ExtractToken(payload)
		if !ok {
			return
		}
		// constant-time compare so timing doesn't leak token bytes
		if subtle.ConstantTimeCompare([]byte(got), []byte(pairTok)) != 1 {
			_, _ = api.SendMessage(ctx, &tgbot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "Invalid pairing link.",
			})
			return
		}
		if err := Consume(ctx, db, got); err != nil {
			_, _ = api.SendMessage(ctx, &tgbot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "Pairing link expired or already used.",
			})
			errCh <- err
			return
		}
		if err := telegram.RegisterCommands(ctx, api); err != nil {
			fmt.Fprintf(io.Err, "warning: could not register Telegram commands: %v\n", err)
		}
		_, _ = api.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "Paired. This chat is now the owner channel for Onibi.",
		})
		doneCh <- update.Message.From.ID
	}

	cli, err := newPairClient(ctx, token, handler)
	if err != nil {
		return 0, err
	}
	// persist bot_id so rotate-token can refuse a different bot's token
	if err := db.KVSetString(ctx, kvKeyBotID, fmt.Sprintf("%d", cli.Self().ID)); err != nil {
		return 0, err
	}

	deepLink := DeepLink(cli.Self().Username, pairTok)
	fmt.Fprintln(io.Out, "")
	fmt.Fprintln(io.Out, "2) Tap this link on your phone (or scan the QR below):")
	fmt.Fprintf(io.Out, "   %s\n\n", deepLink)
	if err := PrintQR(io.Out, deepLink); err != nil {
		// non-fatal: link still works
		fmt.Fprintf(io.Err, "(QR render failed: %v — use the link above)\n", err)
	}
	fmt.Fprintln(io.Out, "")
	fmt.Fprintf(io.Out, "Waiting for you to tap Start (up to %s)...\n", trimDur(time.Until(deadlineOf(ctx))))

	go cli.Start(ctx)

	select {
	case chatID := <-doneCh:
		return chatID, nil
	case err := <-errCh:
		return 0, err
	case <-ctx.Done():
		return 0, fmt.Errorf("pair timed out: %w", ctx.Err())
	}
}

func enableTOTP(ctx context.Context, sec *secrets.Store, io IO) error {
	secret, err := auth.NewSecret()
	if err != nil {
		return err
	}
	if err := sec.Set(secrets.KeyTOTPSecret, auth.EncodeHex(secret)); err != nil {
		return err
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "this-machine"
	}
	uri := auth.OTPAuthURI(secret, "onibi@"+host, "onibi")
	fmt.Fprintln(io.Out, "")
	fmt.Fprintln(io.Out, "TOTP enabled. Scan this QR with Google Authenticator / 1Password / Authy:")
	if err := PrintQR(io.Out, uri); err != nil {
		fmt.Fprintf(io.Err, "(QR failed: %v)\n", err)
		fmt.Fprintf(io.Out, "Or enter this URI manually: %s\n", uri)
	}
	_ = ctx // placeholder for future per-tx setup
	return nil
}

func acknowledge2FA(ctx context.Context, db *store.DB, io IO) error {
	fmt.Fprintln(io.Out, "")
	fmt.Fprintln(io.Out, "Strongly recommended: enable Telegram 2-step verification.")
	fmt.Fprintln(io.Out, "  Telegram → Settings → Privacy and Security → Two-Step Verification")
	fmt.Fprintln(io.Out, "  Use an EMAIL recovery address, not SMS (SIM-swap risk).")
	fmt.Fprintln(io.Out, "")
	fmt.Fprint(io.Out, "Type 'enabled' if you've turned it on, or 'skip' to acknowledge later: ")
	br := bufio.NewReader(io.In)
	for {
		line, err := br.ReadString('\n')
		if err != nil && err != io2EOF() {
			return err
		}
		switch strings.TrimSpace(strings.ToLower(line)) {
		case "enabled":
			return db.KVSetString(ctx, "tg_2fa_ack", "enabled")
		case "skip":
			return db.KVSetString(ctx, "tg_2fa_ack", "skipped")
		default:
			fmt.Fprint(io.Out, "Please type 'enabled' or 'skip': ")
		}
	}
}

func readAllTrimmed(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func io2EOF() error { return io.EOF }

func deadlineOf(ctx context.Context) time.Time {
	d, ok := ctx.Deadline()
	if !ok {
		return time.Now().Add(10 * time.Minute)
	}
	return d
}

func trimDur(d time.Duration) string { return d.Truncate(time.Second).String() }
