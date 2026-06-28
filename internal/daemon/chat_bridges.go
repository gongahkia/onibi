package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/discord"
	"github.com/gongahkia/onibi/internal/gotify"
	"github.com/gongahkia/onibi/internal/matrix"
	"github.com/gongahkia/onibi/internal/ntfy"
	"github.com/gongahkia/onibi/internal/pushover"
	"github.com/gongahkia/onibi/internal/slack"
)

const matrixKVSince = "matrix.since"

func (d *Daemon) runMatrixBridge(ctx context.Context, c *matrix.Client) error {
	if c == nil {
		return errors.New("matrix client nil")
	}
	if strings.TrimSpace(d.Matrix.RoomID) == "" {
		return errors.New("matrix room id required")
	}
	if _, err := c.CheckRoomOwner(ctx, d.Matrix.RoomID, 50); err != nil {
		return err
	}
	go d.forwardApprovalsToMatrix(ctx, c)
	since := ""
	if d.DB != nil {
		since, _, _ = d.DB.KVGetString(ctx, matrixKVSince)
	}
	for {
		sync, err := c.Sync(ctx, since, 25*time.Second)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
				continue
			}
		}
		if sync.NextBatch != "" {
			since = sync.NextBatch
			if d.DB != nil {
				_ = d.DB.KVSetString(ctx, matrixKVSince, since)
			}
		}
		room := sync.Rooms.Join[d.Matrix.RoomID]
		for _, ev := range room.Timeline.Events {
			body := matrix.MessageBody(ev)
			if body == "" || (d.Matrix.OwnerUserID != "" && ev.Sender != d.Matrix.OwnerUserID) {
				continue
			}
			out, err := d.handleProviderText(ctx, "", body, 0)
			if err != nil {
				_ = c.SendText(ctx, d.Matrix.RoomID, "Input failed: "+err.Error())
				continue
			}
			_ = c.SendText(ctx, d.Matrix.RoomID, out)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func (d *Daemon) forwardApprovalsToMatrix(ctx context.Context, c *matrix.Client) {
	if d.Queue == nil {
		return
	}
	events, unsub := d.Queue.Subscribe()
	defer unsub()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Type == approval.EventRequested {
				a := ev.Approval
				_ = c.SendText(ctx, d.Matrix.RoomID, formatApproval(&a))
			}
		}
	}
}

func (d *Daemon) runSlackBridge(ctx context.Context, c *slack.Client) error {
	if c == nil {
		return errors.New("slack client nil")
	}
	url, err := c.OpenSocket(ctx)
	if err != nil {
		return err
	}
	allow := slack.Allowlist{Channels: set(d.Slack.AllowedIDs), DMUsers: set(d.Slack.AllowedDMUsers)}
	for {
		conn, err := slack.Dial(ctx, url)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
				continue
			}
		}
		err = d.runSlackSocket(ctx, c, conn, allow)
		_ = conn.CloseNow()
		if errors.Is(err, context.Canceled) {
			return err
		}
	}
}

func (d *Daemon) runSlackSocket(ctx context.Context, c *slack.Client, conn *websocket.Conn, allow slack.Allowlist) error {
	for {
		env, err := slack.ReadEnvelope(ctx, conn)
		if err != nil {
			return err
		}
		_ = slack.Ack(ctx, conn, env.EnvelopeID, nil)
		switch env.Type {
		case "events_api":
			ev, err := slack.ParseEvent(env)
			if err != nil || ev.Event.Type != "message" || strings.TrimSpace(ev.Event.Text) == "" || !allow.Allows(ev.Event.Channel, ev.Event.User, ev.Event.ChannelType) {
				continue
			}
			out, err := d.handleProviderText(ctx, "", ev.Event.Text, 0)
			if err != nil {
				_ = c.PostMessage(ctx, ev.Event.Channel, "Input failed: "+err.Error())
				continue
			}
			_ = c.PostMessage(ctx, ev.Event.Channel, out)
		case "interactive":
			action, err := slack.ParseInteraction(env)
			if err == nil && len(action.Actions) > 0 {
				d.handleProviderApproval(ctx, action.Actions[0].ActionID, action.Actions[0].Value, 0)
			}
		}
	}
}

func (d *Daemon) runDiscordBridge(ctx context.Context, c *discord.Client) error {
	gatewayURL := strings.TrimSpace(d.Discord.GatewayURL)
	if gatewayURL == "" {
		gatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"
	}
	allow := set(d.Discord.AllowedIDs)
	intents := d.Discord.Intents
	if intents == 0 {
		intents = 1 << 9
	}
	for {
		conn, err := discord.DialGateway(ctx, gatewayURL)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
				continue
			}
		}
		_, _ = discord.ReadFrame(ctx, conn)
		_ = discord.SendIdentify(ctx, conn, d.Discord.Token, intents)
		err = d.runDiscordSocket(ctx, c, conn, allow)
		_ = conn.CloseNow()
		if errors.Is(err, context.Canceled) {
			return err
		}
	}
}

func (d *Daemon) runDiscordSocket(ctx context.Context, c *discord.Client, conn *websocket.Conn, allow map[string]bool) error {
	for {
		frame, err := discord.ReadFrame(ctx, conn)
		if err != nil {
			return err
		}
		if discord.HandleReconnect(frame) {
			return nil
		}
		if msg, ok, err := discord.ParseMessage(frame); err == nil && ok {
			if len(allow) > 0 && !allow[msg.ChannelID] && !allow[msg.Author.ID] {
				continue
			}
			if discord.MissingMessageContent(msg) {
				_ = c.CreateMessage(ctx, msg.ChannelID, "Message content intent is missing. Use slash commands or enable the intent.")
				continue
			}
			out, err := d.handleProviderText(ctx, "", msg.Content, 0)
			if err != nil {
				_ = c.CreateMessage(ctx, msg.ChannelID, "Input failed: "+err.Error())
				continue
			}
			_ = c.CreateMessage(ctx, msg.ChannelID, out)
		}
		if in, ok, err := discord.ParseInteraction(frame); err == nil && ok {
			_ = c.RespondInteraction(ctx, in.ID, in.Token, "Slash command received.")
		}
	}
}

func (d *Daemon) runPushoverNotifier(ctx context.Context, c *pushover.Client) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		resp, err := c.Send(ctx, pushover.MessageOptions{Title: "Onibi approval", Message: formatApproval(a), Priority: 2, Retry: 30 * time.Second, Expire: time.Hour})
		if err == nil && resp.Receipt != "" {
			go func() { _, _ = c.PollReceipt(ctx, resp.Receipt, 30*time.Second) }()
		}
	})
}

func (d *Daemon) runNtfyNotifier(ctx context.Context, c *ntfy.Client) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		_ = c.Publish(ctx, ntfy.Message{Title: "Onibi approval", Body: formatApproval(a), Tags: "warning"})
	})
}

func (d *Daemon) runGotifyNotifier(ctx context.Context, c *gotify.Client) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		_ = c.Send(ctx, gotify.Message{Title: "Onibi approval", Message: formatApproval(a), Priority: 8})
	})
}

func (d *Daemon) forwardNotifyApprovals(ctx context.Context, send func(*approval.Approval)) {
	if d.Queue == nil {
		return
	}
	events, unsub := d.Queue.Subscribe()
	defer unsub()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Type == approval.EventRequested {
				a := ev.Approval
				send(&a)
			}
		}
	}
}

func (d *Daemon) handleProviderText(ctx context.Context, target, text string, actor int64) (string, error) {
	if handled, reply := d.handleProviderTextCommand(ctx, text, actor); handled {
		return reply, nil
	}
	return d.SendSessionTextAndCapture(ctx, target, text, true)
}

func (d *Daemon) handleProviderTextCommand(ctx context.Context, text string, actor int64) (bool, string) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false, ""
	}
	verb := strings.TrimPrefix(strings.ToLower(fields[0]), "/")
	var verdict approval.Verdict
	switch verb {
	case "approve", "ap":
		verdict = approval.VerdictApprove
	case "deny", "dn":
		verdict = approval.VerdictDeny
	default:
		return false, ""
	}
	if len(fields) < 2 {
		return true, "Approval id required."
	}
	id := fields[1]
	if err := d.decideProviderApproval(ctx, id, verdict, actor); err != nil {
		return true, fmt.Sprintf("Approval %s failed: %v", id, err)
	}
	return true, fmt.Sprintf("Approval %s %s.", id, verdict)
}

func (d *Daemon) handleProviderApproval(ctx context.Context, action, id string, actor int64) {
	switch strings.ToLower(action) {
	case "approve", "ap":
		_ = d.decideProviderApproval(ctx, id, approval.VerdictApprove, actor)
	case "deny", "dn":
		_ = d.decideProviderApproval(ctx, id, approval.VerdictDeny, actor)
	}
}

func (d *Daemon) decideProviderApproval(ctx context.Context, id string, verdict approval.Verdict, actor int64) error {
	if d.Queue == nil || strings.TrimSpace(id) == "" {
		return errors.New("approval queue/id required")
	}
	return d.Queue.Decide(ctx, id, verdict, "", fmt.Sprintf("provider %s", verdict), actor)
}

func set(vals []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			out[v] = true
		}
	}
	return out
}
