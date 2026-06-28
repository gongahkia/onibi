package daemon

import (
	"context"
	"encoding/json"
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
	encrypted, err := c.IsEncryptedRoom(ctx, d.Matrix.RoomID)
	if err != nil {
		return err
	}
	if encrypted && !d.Matrix.AllowEncrypted {
		return errors.New("matrix encrypted rooms require real Olm/Megolm E2EE; use an unencrypted room or set ONIBI_MATRIX_ALLOW_ENCRYPTED=1 only for send-only testing")
	}
	go d.forwardApprovalsToMatrix(ctx, c)
	since := ""
	if d.DB != nil {
		since, _, _ = d.DB.KVGetString(ctx, matrixKVSince)
	}
	for {
		sync, err := c.SyncRoom(ctx, d.Matrix.RoomID, since, 25*time.Second)
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
				_ = c.SendText(ctx, d.Matrix.RoomID, formatApprovalWithPolicy(&a, d.ProviderOutput))
			}
		}
	}
}

func (d *Daemon) runSlackBridge(ctx context.Context, c *slack.Client) error {
	if c == nil {
		return errors.New("slack client nil")
	}
	allow := slack.Allowlist{Channels: set(d.Slack.AllowedIDs), DMUsers: set(d.Slack.AllowedDMUsers)}
	if ch := d.slackApprovalChannel(); ch != "" {
		go d.forwardApprovalsToSlack(ctx, c, ch)
	}
	for {
		url, err := c.OpenSocket(ctx)
		if err != nil {
			return err
		}
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
		switch env.Type {
		case "disconnect":
			_ = slack.Ack(ctx, conn, env.EnvelopeID, nil)
			if slack.ShouldReconnect(env) {
				return nil
			}
		case "events_api":
			_ = slack.Ack(ctx, conn, env.EnvelopeID, nil)
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
			ackPayload := map[string]any{"text": "Approval decision received."}
			if err == nil && len(action.Actions) > 0 {
				id := slackApprovalID(action.Actions[0].Value)
				if err := d.decideProviderApproval(ctx, id, approvalVerdictForAction(action.Actions[0].ActionID), 0); err != nil {
					ackPayload["text"] = "Approval decision failed: " + err.Error()
				}
			}
			if !env.Accepts {
				ackPayload = nil
			}
			_ = slack.Ack(ctx, conn, env.EnvelopeID, ackPayload)
		default:
			_ = slack.Ack(ctx, conn, env.EnvelopeID, nil)
		}
	}
}

func (d *Daemon) slackApprovalChannel() string {
	ch := strings.TrimSpace(d.Slack.ApprovalChannel)
	if ch != "" {
		return ch
	}
	if len(d.Slack.AllowedIDs) > 0 {
		return strings.TrimSpace(d.Slack.AllowedIDs[0])
	}
	return ""
}

func (d *Daemon) forwardApprovalsToSlack(ctx context.Context, c *slack.Client, channel string) {
	if d.Queue == nil || strings.TrimSpace(channel) == "" {
		return
	}
	seen := map[string]bool{}
	send := func(a *approval.Approval) {
		if a == nil {
			return
		}
		if seen[a.ID] {
			return
		}
		seen[a.ID] = true
		_ = c.PostMessageBlocks(ctx, channel, "Onibi approval "+a.ID, slackApprovalBlocks(a, d.ProviderOutput))
	}
	if pending, err := d.Queue.Pending(ctx); err == nil {
		for _, a := range pending {
			send(a)
		}
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

func slackApprovalBlocks(a *approval.Approval, policy ProviderOutputPolicy) []any {
	text := formatApprovalWithPolicy(a, policy)
	if len(text) > 2800 {
		text = text[:2800] + "\n..."
	}
	return []any{
		map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "```" + text + "```"}},
		map[string]any{"type": "actions", "block_id": "approval:" + a.ID, "elements": []any{
			map[string]any{
				"type": "button", "text": map[string]any{"type": "plain_text", "text": "Approve"},
				"style": "primary", "action_id": "approve", "value": slackApprovalValue(a, approval.VerdictApprove),
			},
			map[string]any{
				"type": "button", "text": map[string]any{"type": "plain_text", "text": "Deny"},
				"style": "danger", "action_id": "deny", "value": slackApprovalValue(a, approval.VerdictDeny),
			},
		}},
	}
}

func slackApprovalValue(a *approval.Approval, verdict approval.Verdict) string {
	b, err := json.Marshal(map[string]string{
		"approval_id": a.ID,
		"session_id":  a.SessionID,
		"agent":       a.Agent,
		"tool":        a.Tool,
		"verdict":     string(verdict),
	})
	if err != nil {
		return a.ID
	}
	return string(b)
}

func slackApprovalID(value string) string {
	var payload struct {
		ApprovalID string `json:"approval_id"`
	}
	if err := json.Unmarshal([]byte(value), &payload); err == nil && payload.ApprovalID != "" {
		return payload.ApprovalID
	}
	return value
}

func (d *Daemon) runDiscordBridge(ctx context.Context, c *discord.Client) error {
	gatewayURL := strings.TrimSpace(d.Discord.GatewayURL)
	if gatewayURL == "" {
		gatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"
	}
	allow := set(d.Discord.AllowedIDs)
	intents := d.Discord.Intents
	if intents == 0 {
		intents = (1 << 9) | (1 << 12) | (1 << 15)
	}
	state := &discord.GatewayState{}
	for {
		connectURL := gatewayURL
		if resumeURL, _, _, ok := state.Resume(gatewayURL); ok {
			connectURL = resumeURL
		}
		conn, err := discord.DialGateway(ctx, connectURL)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
				continue
			}
		}
		helloFrame, err := discord.ReadFrame(ctx, conn)
		if err != nil {
			_ = conn.CloseNow()
			continue
		}
		state.Observe(helloFrame)
		hello, _, _ := discord.ParseHello(helloFrame)
		socketCtx, stopSocket := context.WithCancel(ctx)
		if hello.HeartbeatInterval > 0 {
			go func() {
				if err := d.runDiscordHeartbeat(socketCtx, conn, time.Duration(hello.HeartbeatInterval)*time.Millisecond, state); err != nil && !errors.Is(err, context.Canceled) {
					d.Log.Warn("discord heartbeat", "err", err)
				}
			}()
		}
		if _, sessionID, seq, ok := state.Resume(gatewayURL); ok {
			_ = discord.SendResume(ctx, conn, d.Discord.Token, sessionID, seq)
		} else {
			_ = discord.SendIdentify(ctx, conn, d.Discord.Token, intents)
		}
		err = d.runDiscordSocket(socketCtx, c, conn, allow, state)
		stopSocket()
		_ = conn.CloseNow()
		if errors.Is(err, context.Canceled) {
			return err
		}
	}
}

func (d *Daemon) runDiscordHeartbeat(ctx context.Context, conn *websocket.Conn, interval time.Duration, state *discord.GatewayState) error {
	if interval <= 0 {
		return nil
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if state.AckOverdue(2 * interval) {
				_ = conn.Close(websocket.StatusGoingAway, "discord heartbeat ack timeout")
				return errors.New("discord heartbeat ack timeout")
			}
			if err := discord.SendHeartbeat(ctx, conn, state.HeartbeatSeq()); err != nil {
				return err
			}
			state.MarkHeartbeatSent()
		}
	}
}

func (d *Daemon) runDiscordSocket(ctx context.Context, c *discord.Client, conn *websocket.Conn, allow map[string]bool, state *discord.GatewayState) error {
	for {
		frame, err := discord.ReadFrame(ctx, conn)
		if err != nil {
			return err
		}
		state.Observe(frame)
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
			text := discord.InteractionText(in)
			if strings.EqualFold(in.Data.Name, "onibi") && text != "" {
				out, err := d.handleProviderText(ctx, "", text, 0)
				if err != nil {
					out = "Input failed: " + err.Error()
				}
				_ = c.RespondInteraction(ctx, in.ID, in.Token, out)
			} else {
				_ = c.RespondInteraction(ctx, in.ID, in.Token, "Slash command received.")
			}
		}
	}
}

func (d *Daemon) runPushoverNotifier(ctx context.Context, c *pushover.Client) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		d.sendPushoverApproval(ctx, c, a)
	})
}

func (d *Daemon) sendPushoverApproval(ctx context.Context, c *pushover.Client, a *approval.Approval) {
	resp, err := c.Send(ctx, pushover.MessageOptions{Title: "Onibi approval", Message: formatApprovalWithPolicy(a, d.ProviderOutput), Priority: 2, Retry: 30 * time.Second, Expire: time.Hour})
	if err != nil {
		d.audit(ctx, "notify.pushover.error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
		return
	}
	if resp.Receipt == "" {
		d.audit(ctx, "notify.pushover.sent", a.SessionID, "", 0, "approval="+a.ID+" receipt=false")
		return
	}
	d.audit(ctx, "notify.pushover.receipt", a.SessionID, "", 0, "approval="+a.ID+" receipt="+resp.Receipt)
	go func() {
		got, err := c.PollReceipt(ctx, resp.Receipt, 30*time.Second)
		if err != nil {
			d.audit(ctx, "notify.pushover.receipt.error", a.SessionID, "", 0, "approval="+a.ID+" receipt="+resp.Receipt+" err="+err.Error())
			return
		}
		state := "pending"
		if got.Acknowledged == 1 {
			state = "acknowledged"
		} else if got.Expired == 1 {
			state = "expired"
		}
		d.audit(ctx, "notify.pushover.receipt."+state, a.SessionID, "", 0, "approval="+a.ID+" receipt="+resp.Receipt)
	}()
}

func (d *Daemon) runNtfyNotifier(ctx context.Context, c *ntfy.Client) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		if err := c.Publish(ctx, ntfy.Message{Title: "Onibi approval", Body: formatApprovalWithPolicy(a, d.ProviderOutput), Tags: "warning"}); err != nil {
			d.audit(ctx, "notify.ntfy.error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
			return
		}
		d.audit(ctx, "notify.ntfy.sent", a.SessionID, "", 0, "approval="+a.ID)
	})
}

func (d *Daemon) runGotifyNotifier(ctx context.Context, c *gotify.Client) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		if err := c.Send(ctx, gotify.Message{Title: "Onibi approval", Message: formatApprovalWithPolicy(a, d.ProviderOutput), Priority: 8}); err != nil {
			d.audit(ctx, "notify.gotify.error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
			return
		}
		d.audit(ctx, "notify.gotify.sent", a.SessionID, "", 0, "approval="+a.ID)
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
		return d.prepareProviderOutput(reply), nil
	}
	out, err := d.SendSessionTextAndCapture(ctx, target, text, true)
	return d.prepareProviderOutput(out), err
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
	verdict := approvalVerdictForAction(action)
	if verdict == "" {
		return
	}
	_ = d.decideProviderApproval(ctx, id, verdict, actor)
}

func approvalVerdictForAction(action string) approval.Verdict {
	switch strings.ToLower(action) {
	case "approve", "ap":
		return approval.VerdictApprove
	case "deny", "dn":
		return approval.VerdictDeny
	default:
		return ""
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
