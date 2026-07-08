//go:build !onibi_remote

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
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
	"github.com/gongahkia/onibi/internal/web"
)

const matrixKVSince = "matrix.since"

const (
	slackEditCallback     = "onibi_approval_edit"
	slackEditInputBlock   = "edited_input"
	slackEditInputAction  = "json"
	slackReconnectMaxWait = 30 * time.Second
)

const (
	discordApprovalPrefix   = "onibi:approval:"
	discordEditModalPrefix  = "onibi:approval_edit:"
	discordEditInputID      = "json"
	discordReconnectMaxWait = 30 * time.Second
)

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
			out, err := d.handleProviderTextFor(ctx, "", body, 0, "matrix")
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
	events, unsub, err := d.Queue.Subscribe()
	if err != nil {
		if d.Log != nil {
			d.Log.Warn("matrix approval subscribe failed", "err", err)
		}
		return
	}
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
				_ = c.SendText(ctx, d.Matrix.RoomID, formatApprovalWithPolicy(&a, d.providerOutputPolicy("matrix")))
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
	attempt := 0
	for {
		url, err := c.OpenSocket(ctx)
		if err != nil {
			return err
		}
		conn, err := slack.Dial(ctx, url)
		if err != nil {
			if sleepErr := slackReconnectSleep(ctx, c, attempt); sleepErr != nil {
				return sleepErr
			}
			attempt++
			continue
		}
		err = d.runSlackSocket(ctx, c, conn, allow)
		_ = conn.CloseNow()
		if errors.Is(err, context.Canceled) {
			return err
		}
		if sleepErr := slackReconnectSleep(ctx, c, attempt); sleepErr != nil {
			return sleepErr
		}
		attempt++
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
			sessionID := d.providerTargetSessionID("")
			d.audit(ctx, "provider.slack.text_in", sessionID, ev.Event.Text, 0, "channel="+ev.Event.Channel+" user="+ev.Event.User)
			out, err := d.handleProviderTextFor(ctx, "", ev.Event.Text, 0, "slack")
			if err != nil {
				_ = c.PostMessage(ctx, ev.Event.Channel, "Input failed: "+err.Error())
				continue
			}
			d.postSlackTail(ctx, c, ev.Event.Channel, sessionID, out)
		case "interactive":
			action, err := slack.ParseInteraction(env)
			ackPayload := map[string]any{"text": "Approval interaction failed."}
			if err == nil {
				ackPayload = d.handleSlackInteraction(ctx, c, action)
			}
			if !env.Accepts {
				ackPayload = nil
			}
			if ackPayload == nil {
				_ = slack.Ack(ctx, conn, env.EnvelopeID, nil)
			} else {
				_ = slack.Ack(ctx, conn, env.EnvelopeID, ackPayload)
			}
		default:
			_ = slack.Ack(ctx, conn, env.EnvelopeID, nil)
		}
	}
}

func slackReconnectDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 5 {
		attempt = 5
	}
	base := time.Second << attempt
	if base > slackReconnectMaxWait {
		base = slackReconnectMaxWait
	}
	jitter := time.Duration(rand.Int63n(int64(base/2) + 1))
	return base + jitter
}

func slackReconnectSleep(ctx context.Context, c *slack.Client, attempt int) error {
	delay := slackReconnectDelay(attempt)
	if c != nil && c.Sleep != nil {
		return c.Sleep(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (d *Daemon) handleSlackInteraction(ctx context.Context, c *slack.Client, action slack.InteractionPayload) map[string]any {
	switch action.Type {
	case "view_submission":
		return d.handleSlackEditSubmission(ctx, c, action)
	default:
		return map[string]any{"text": d.handleSlackBlockAction(ctx, c, action)}
	}
}

func (d *Daemon) handleSlackBlockAction(ctx context.Context, c *slack.Client, action slack.InteractionPayload) string {
	if len(action.Actions) == 0 {
		return "Approval decision failed: invalid action."
	}
	raw := action.Actions[0]
	id := slackApprovalID(raw.Value)
	sessionID := d.approvalSessionID(ctx, id)
	d.audit(ctx, "provider.slack.button", sessionID, raw.Value, 0, "action="+raw.ActionID+" approval="+id+" user="+action.User.ID+" channel="+action.Channel.ID)
	switch strings.ToLower(raw.ActionID) {
	case "approve", "deny":
		verdict := approvalVerdictForAction(raw.ActionID)
		return d.handleSlackApprovalDecision(ctx, c, id, verdict, action.Channel.ID, action.Message.TS)
	case "edit":
		return d.openSlackEditModal(ctx, c, id, action.TriggerID, action.User.ID)
	default:
		return "Approval decision failed: invalid action."
	}
}

func (d *Daemon) approvalSessionID(ctx context.Context, id string) string {
	if d == nil || d.Queue == nil || id == "" {
		return ""
	}
	a, err := d.Queue.Get(ctx, id)
	if err != nil {
		return ""
	}
	return a.SessionID
}

func (d *Daemon) providerTargetSessionID(target string) string {
	if d == nil {
		return ""
	}
	s, err := d.sessionForRPCTarget(target)
	if err != nil {
		return ""
	}
	return s.ID
}

func (d *Daemon) postSlackTail(ctx context.Context, c *slack.Client, channel, sessionID, text string) {
	if c == nil {
		return
	}
	err := c.PostMessageChunks(ctx, channel, text, func(i int, chunk string) {
		d.audit(ctx, "provider.slack.tail_chunk", sessionID, chunk, 0, fmt.Sprintf("channel=%s index=%d bytes=%d", channel, i, len(chunk)))
	})
	if err != nil {
		d.audit(ctx, "provider.slack.tail_error", sessionID, "", 0, "channel="+channel+" err="+err.Error())
	}
}

func (d *Daemon) openSlackEditModal(ctx context.Context, c *slack.Client, id, triggerID, user string) string {
	if d == nil || d.Queue == nil {
		return "Edit failed: approval queue unavailable."
	}
	a, err := d.Queue.Get(ctx, id)
	if err != nil {
		return "Edit failed: " + err.Error()
	}
	if _, err := c.OpenView(ctx, triggerID, slackEditModalView(a)); err != nil {
		d.audit(ctx, "provider.slack.edit_modal_error", a.SessionID, "", 0, "approval="+id+" user="+user+" err="+err.Error())
		return "Edit failed: " + err.Error()
	}
	d.audit(ctx, "provider.slack.edit_modal", a.SessionID, "", 0, "approval="+id+" user="+user)
	return "Edit modal opened for approval " + id + "."
}

func (d *Daemon) handleSlackEditSubmission(ctx context.Context, c *slack.Client, action slack.InteractionPayload) map[string]any {
	id, edited := slackEditSubmission(action)
	if id == "" {
		return slackModalError("approval id missing")
	}
	if d == nil || d.Queue == nil {
		return slackModalError("approval queue unavailable")
	}
	a, err := d.Queue.Get(ctx, id)
	if err != nil {
		return slackModalError(err.Error())
	}
	edited = strings.TrimSpace(edited)
	if edited == "" {
		return slackModalError("edited JSON required")
	}
	if err := approval.ValidateEditedInput(a.Tool, a.InputJSON, edited); err != nil {
		d.audit(ctx, "provider.slack.edit_rejected", a.SessionID, edited, 0, "approval="+id+" user="+action.User.ID+" err="+err.Error())
		return slackModalError(err.Error())
	}
	if err := d.Queue.Decide(ctx, id, approval.VerdictEdit, edited, "provider edit", 0); err != nil {
		return slackModalError(err.Error())
	}
	d.audit(ctx, "provider.slack.edit_submit", a.SessionID, edited, 0, "approval="+id+" user="+action.User.ID)
	d.updateSlackApprovalMessage(ctx, c, id, approval.StateEdited, "edited via Slack", "", "")
	return nil
}

func slackEditSubmission(action slack.InteractionPayload) (string, string) {
	if action.View.CallbackID != slackEditCallback {
		return "", ""
	}
	edited := ""
	if actions, ok := action.View.State.Values[slackEditInputBlock]; ok {
		if value, ok := actions[slackEditInputAction]; ok {
			edited = value.Value
		}
	}
	return strings.TrimSpace(action.View.PrivateMetadata), edited
}

func slackModalError(message string) map[string]any {
	return map[string]any{
		"response_action": "errors",
		"errors":          map[string]string{slackEditInputBlock: message},
	}
}

func (d *Daemon) handleSlackApprovalDecision(ctx context.Context, c *slack.Client, id string, verdict approval.Verdict, channel, ts string) string {
	if id == "" || verdict == "" {
		return "Approval decision failed: invalid action."
	}
	err := d.decideProviderApproval(ctx, id, verdict, 0)
	state := approval.StateForVerdict(verdict)
	note := "decision recorded"
	if err != nil {
		switch {
		case errors.Is(err, approval.ErrAlreadyDecided):
			note = "already decided"
			if a, getErr := d.Queue.Get(ctx, id); getErr == nil {
				state = a.State
			}
		case errors.Is(err, approval.ErrExpired):
			note = "expired"
			state = approval.StateExpired
		default:
			note = "failed: " + err.Error()
			state = "failed"
		}
	}
	d.updateSlackApprovalMessage(ctx, c, id, state, note, channel, ts)
	if err != nil && !errors.Is(err, approval.ErrAlreadyDecided) && !errors.Is(err, approval.ErrExpired) {
		return "Approval decision failed: " + err.Error()
	}
	return "Approval " + id + ": " + string(state) + " (" + note + ")."
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
		resp, err := c.PostMessageBlocks(ctx, channel, "Onibi approval "+a.ID, slackApprovalBlocks(a, d.providerOutputPolicy("slack")))
		if err != nil {
			d.Log.Warn("slack approval post failed", slog.String("approval_id", a.ID), slog.Any("err", err))
			return
		}
		if resp.Channel != "" && resp.TS != "" {
			d.rememberSlackApproval(a.ID, slackApprovalRef{Channel: resp.Channel, TS: resp.TS})
		}
	}
	if pending, err := d.Queue.Pending(ctx); err == nil {
		for _, a := range pending {
			send(a)
		}
	}
	events, unsub, err := d.Queue.Subscribe()
	if err != nil {
		if d.Log != nil {
			d.Log.Warn("slack approval subscribe failed", "err", err)
		}
		return
	}
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

func (d *Daemon) rememberSlackApproval(id string, ref slackApprovalRef) {
	if d == nil || id == "" || ref.Channel == "" || ref.TS == "" {
		return
	}
	d.slackMu.Lock()
	d.slackApprovals[id] = ref
	d.slackMu.Unlock()
}

func (d *Daemon) slackApprovalRef(id, channel, ts string) (slackApprovalRef, bool) {
	if channel != "" && ts != "" {
		return slackApprovalRef{Channel: channel, TS: ts}, true
	}
	d.slackMu.Lock()
	defer d.slackMu.Unlock()
	ref, ok := d.slackApprovals[id]
	return ref, ok
}

func (d *Daemon) updateSlackApprovalMessage(ctx context.Context, c *slack.Client, id string, state string, note, channel, ts string) {
	if c == nil || d == nil {
		return
	}
	ref, ok := d.slackApprovalRef(id, channel, ts)
	if !ok {
		return
	}
	a, err := d.Queue.Get(ctx, id)
	if err != nil {
		d.Log.Warn("slack approval load failed", slog.String("approval_id", id), slog.Any("err", err))
		return
	}
	blocks := slackApprovalFinalBlocks(a, d.providerOutputPolicy("slack"), state, note)
	if _, err := c.UpdateMessage(ctx, ref.Channel, ref.TS, "Onibi approval "+id+" "+string(state), blocks); err != nil {
		d.Log.Warn("slack approval update failed", slog.String("approval_id", id), slog.Any("err", err))
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
			map[string]any{
				"type": "button", "text": map[string]any{"type": "plain_text", "text": "Edit"},
				"action_id": "edit", "value": slackApprovalValue(a, approval.VerdictEdit),
			},
		}},
	}
}

func slackEditModalView(a *approval.Approval) map[string]any {
	return map[string]any{
		"type":             "modal",
		"callback_id":      slackEditCallback,
		"private_metadata": a.ID,
		"title":            map[string]any{"type": "plain_text", "text": "Edit approval"},
		"submit":           map[string]any{"type": "plain_text", "text": "Submit"},
		"close":            map[string]any{"type": "plain_text", "text": "Cancel"},
		"blocks": []any{
			map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "Edit JSON for approval `" + a.ID + "`."}},
			map[string]any{
				"type":     "input",
				"block_id": slackEditInputBlock,
				"label":    map[string]any{"type": "plain_text", "text": "JSON"},
				"element": map[string]any{
					"type":          "plain_text_input",
					"action_id":     slackEditInputAction,
					"multiline":     true,
					"initial_value": a.InputJSON,
				},
			},
		},
	}
}

func slackApprovalFinalBlocks(a *approval.Approval, policy ProviderOutputPolicy, state string, note string) []any {
	text := formatApprovalWithPolicy(a, policy)
	if len(text) > 2600 {
		text = text[:2600] + "\n..."
	}
	if strings.TrimSpace(note) == "" {
		note = string(state)
	}
	return []any{
		map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "```" + text + "```"}},
		map[string]any{"type": "context", "elements": []any{map[string]any{"type": "mrkdwn", "text": "*State:* " + string(state) + "  *Note:* " + note}}},
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
	if ch := d.discordApprovalChannel(); ch != "" {
		go d.forwardApprovalsToDiscord(ctx, c, ch)
	}
	state := &discord.GatewayState{}
	attempt := 0
	for {
		connectURL := gatewayURL
		if resumeURL, _, _, ok := state.Resume(gatewayURL); ok {
			connectURL = resumeURL
		}
		conn, err := discord.DialGateway(ctx, connectURL)
		if err != nil {
			if sleepErr := discordReconnectSleep(ctx, c, attempt); sleepErr != nil {
				return sleepErr
			}
			attempt++
			continue
		}
		helloFrame, err := discord.ReadFrame(ctx, conn)
		if err != nil {
			_ = conn.CloseNow()
			if sleepErr := discordReconnectSleep(ctx, c, attempt); sleepErr != nil {
				return sleepErr
			}
			attempt++
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
		if sleepErr := discordReconnectSleep(ctx, c, attempt); sleepErr != nil {
			return sleepErr
		}
		attempt++
	}
}

func discordReconnectDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 5 {
		attempt = 5
	}
	base := time.Second << attempt
	if base > discordReconnectMaxWait {
		base = discordReconnectMaxWait
	}
	jitter := time.Duration(rand.Int63n(int64(base/2) + 1))
	return base + jitter
}

func discordReconnectSleep(ctx context.Context, c *discord.Client, attempt int) error {
	delay := discordReconnectDelay(attempt)
	if c != nil && c.Sleep != nil {
		return c.Sleep(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
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
			sessionID := d.providerTargetSessionID("")
			d.audit(ctx, "provider.discord.text_in", sessionID, msg.Content, 0, "channel="+msg.ChannelID+" user="+msg.Author.ID)
			out, err := d.handleProviderTextFor(ctx, "", msg.Content, 0, "discord")
			if err != nil {
				_ = c.CreateMessage(ctx, msg.ChannelID, "Input failed: "+err.Error())
				continue
			}
			d.postDiscordTail(ctx, c, msg.ChannelID, sessionID, out)
		}
		if in, ok, err := discord.ParseInteraction(frame); err == nil && ok {
			if d.handleDiscordInteraction(ctx, c, in) {
				continue
			}
			text := discord.InteractionText(in)
			if strings.EqualFold(in.Data.Name, "onibi") && text != "" {
				sessionID := d.providerTargetSessionID("")
				d.audit(ctx, "provider.discord.text_in", sessionID, text, 0, "interaction="+in.ID+" user="+discord.InteractionUserID(in))
				out, err := d.handleProviderTextFor(ctx, "", text, 0, "discord")
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

func (d *Daemon) discordApprovalChannel() string {
	if len(d.Discord.AllowedIDs) > 0 {
		return strings.TrimSpace(d.Discord.AllowedIDs[0])
	}
	return ""
}

func (d *Daemon) forwardApprovalsToDiscord(ctx context.Context, c *discord.Client, channel string) {
	if d.Queue == nil || strings.TrimSpace(channel) == "" {
		return
	}
	seen := map[string]bool{}
	send := func(a *approval.Approval) {
		if a == nil || seen[a.ID] {
			return
		}
		seen[a.ID] = true
		msg, err := c.CreateComponentsMessage(ctx, channel, discordApprovalComponents(a, d.providerOutputPolicy("discord")))
		if err != nil {
			d.audit(ctx, "provider.discord.approval_error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
			return
		}
		d.rememberDiscordApproval(a.ID, discordApprovalRef{Channel: msg.ChannelID, Message: msg.ID})
		d.audit(ctx, "provider.discord.approval_sent", a.SessionID, "", 0, "approval="+a.ID+" channel="+msg.ChannelID+" message="+msg.ID)
	}
	if pending, err := d.Queue.Pending(ctx); err == nil {
		for _, a := range pending {
			send(a)
		}
	}
	events, unsub, err := d.Queue.Subscribe()
	if err != nil {
		if d.Log != nil {
			d.Log.Warn("discord approval subscribe failed", "err", err)
		}
		return
	}
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

func (d *Daemon) rememberDiscordApproval(id string, ref discordApprovalRef) {
	if d == nil || id == "" || ref.Channel == "" || ref.Message == "" {
		return
	}
	d.discordMu.Lock()
	d.discordApprovals[id] = ref
	d.discordMu.Unlock()
}

func (d *Daemon) handleDiscordInteraction(ctx context.Context, c *discord.Client, in discord.Interaction) bool {
	if action, id, ok := discordApprovalAction(in.Data.CustomID); ok {
		d.handleDiscordApprovalAction(ctx, c, in, action, id)
		return true
	}
	if id, ok := discordEditModalID(in.Data.CustomID); ok {
		d.handleDiscordEditSubmit(ctx, c, in, id)
		return true
	}
	return false
}

func discordApprovalAction(customID string) (string, string, bool) {
	rest, ok := strings.CutPrefix(customID, discordApprovalPrefix)
	if !ok {
		return "", "", false
	}
	action, id, ok := strings.Cut(rest, ":")
	if !ok || strings.TrimSpace(id) == "" {
		return "", "", false
	}
	return action, id, true
}

func discordEditModalID(customID string) (string, bool) {
	id, ok := strings.CutPrefix(customID, discordEditModalPrefix)
	id = strings.TrimSpace(id)
	return id, ok && id != ""
}

func (d *Daemon) handleDiscordApprovalAction(ctx context.Context, c *discord.Client, in discord.Interaction, action, id string) {
	sessionID := d.approvalSessionID(ctx, id)
	user := discord.InteractionUserID(in)
	d.audit(ctx, "provider.discord.button", sessionID, in.Data.CustomID, 0, "action="+action+" approval="+id+" user="+user+" channel="+in.ChannelID)
	switch action {
	case "approve", "deny":
		verdict := approvalVerdictForAction(action)
		text := d.handleDiscordApprovalDecision(ctx, id, verdict)
		_ = c.RespondInteraction(ctx, in.ID, in.Token, text)
	case "edit":
		if d.Queue == nil {
			_ = c.RespondInteraction(ctx, in.ID, in.Token, "Edit failed: approval queue unavailable.")
			return
		}
		a, err := d.Queue.Get(ctx, id)
		if err != nil {
			_ = c.RespondInteraction(ctx, in.ID, in.Token, "Edit failed: "+err.Error())
			return
		}
		if err := c.RespondInteractionModal(ctx, in.ID, in.Token, discordEditModal(a)); err != nil {
			d.audit(ctx, "provider.discord.edit_modal_error", a.SessionID, "", 0, "approval="+id+" err="+err.Error())
			return
		}
		d.audit(ctx, "provider.discord.edit_modal", a.SessionID, "", 0, "approval="+id+" user="+user)
	default:
		_ = c.RespondInteraction(ctx, in.ID, in.Token, "Unknown approval action.")
	}
}

func (d *Daemon) handleDiscordApprovalDecision(ctx context.Context, id string, verdict approval.Verdict) string {
	if id == "" || verdict == "" {
		return "Approval decision failed: invalid action."
	}
	err := d.decideProviderApproval(ctx, id, verdict, 0)
	state := approval.StateForVerdict(verdict)
	note := "decision recorded"
	if err != nil {
		switch {
		case errors.Is(err, approval.ErrAlreadyDecided):
			note = "already decided"
			if a, getErr := d.Queue.Get(ctx, id); getErr == nil {
				state = a.State
			}
		case errors.Is(err, approval.ErrExpired):
			note = "expired"
			state = approval.StateExpired
		default:
			return "Approval decision failed: " + err.Error()
		}
	}
	return "Approval " + id + ": " + string(state) + " (" + note + ")."
}

func (d *Daemon) handleDiscordEditSubmit(ctx context.Context, c *discord.Client, in discord.Interaction, id string) {
	if d == nil || d.Queue == nil {
		_ = c.RespondInteraction(ctx, in.ID, in.Token, "Edit failed: approval queue unavailable.")
		return
	}
	a, err := d.Queue.Get(ctx, id)
	if err != nil {
		_ = c.RespondInteraction(ctx, in.ID, in.Token, "Edit failed: "+err.Error())
		return
	}
	edited := discord.InteractionModalValue(in, discordEditInputID)
	if edited == "" {
		_ = c.RespondInteraction(ctx, in.ID, in.Token, "Edit failed: edited JSON required.")
		return
	}
	if err := approval.ValidateEditedInput(a.Tool, a.InputJSON, edited); err != nil {
		d.audit(ctx, "provider.discord.edit_rejected", a.SessionID, edited, 0, "approval="+id+" user="+discord.InteractionUserID(in)+" err="+err.Error())
		_ = c.RespondInteraction(ctx, in.ID, in.Token, "Edit failed: "+err.Error())
		return
	}
	if err := d.Queue.Decide(ctx, id, approval.VerdictEdit, edited, "provider edit", 0); err != nil {
		_ = c.RespondInteraction(ctx, in.ID, in.Token, "Edit failed: "+err.Error())
		return
	}
	d.audit(ctx, "provider.discord.edit_submit", a.SessionID, edited, 0, "approval="+id+" user="+discord.InteractionUserID(in))
	_ = c.RespondInteraction(ctx, in.ID, in.Token, "Approval "+id+": edited.")
}

func (d *Daemon) postDiscordTail(ctx context.Context, c *discord.Client, channel, sessionID, text string) {
	if c == nil {
		return
	}
	target := d.discordTailChannel(ctx, c, channel, sessionID)
	err := c.CreateMessageChunks(ctx, target, text, func(i int, chunk string) {
		d.audit(ctx, "provider.discord.tail_chunk", sessionID, chunk, 0, fmt.Sprintf("channel=%s index=%d bytes=%d", target, i, len(chunk)))
	})
	if err != nil {
		d.audit(ctx, "provider.discord.tail_error", sessionID, "", 0, "channel="+target+" err="+err.Error())
	}
}

func (d *Daemon) discordTailChannel(ctx context.Context, c *discord.Client, parent, sessionID string) string {
	if sessionID == "" {
		return parent
	}
	d.discordMu.Lock()
	thread := d.discordTailThreads[sessionID]
	d.discordMu.Unlock()
	if thread != "" {
		return thread
	}
	seed, err := c.CreateMessagePayload(ctx, parent, map[string]any{
		"content":          "Onibi tail for session " + sessionID,
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
	if err != nil {
		d.audit(ctx, "provider.discord.thread_error", sessionID, "", 0, "channel="+parent+" err="+err.Error())
		return parent
	}
	ch, err := c.StartThreadFromMessage(ctx, parent, seed.ID, "onibi-"+sessionID)
	if err != nil {
		d.audit(ctx, "provider.discord.thread_error", sessionID, "", 0, "channel="+parent+" message="+seed.ID+" err="+err.Error())
		return parent
	}
	d.discordMu.Lock()
	d.discordTailThreads[sessionID] = ch.ID
	d.discordMu.Unlock()
	d.audit(ctx, "provider.discord.thread", sessionID, "", 0, "channel="+parent+" thread="+ch.ID)
	return ch.ID
}

func discordApprovalComponents(a *approval.Approval, policy ProviderOutputPolicy) []any {
	text := formatApprovalWithPolicy(a, policy)
	if len(text) > 1800 {
		text = text[:1800] + "\n..."
	}
	return []any{
		map[string]any{"type": 10, "content": "```" + text + "```"},
		map[string]any{"type": 1, "components": []any{
			map[string]any{"type": 2, "style": 3, "label": "Approve", "custom_id": discordApprovalPrefix + "approve:" + a.ID},
			map[string]any{"type": 2, "style": 4, "label": "Deny", "custom_id": discordApprovalPrefix + "deny:" + a.ID},
			map[string]any{"type": 2, "style": 2, "label": "Edit", "custom_id": discordApprovalPrefix + "edit:" + a.ID},
		}},
	}
}

func discordEditModal(a *approval.Approval) map[string]any {
	return map[string]any{
		"custom_id": discordEditModalPrefix + a.ID,
		"title":     "Edit approval",
		"components": []any{
			map[string]any{"type": 1, "components": []any{
				map[string]any{
					"type":      4,
					"custom_id": discordEditInputID,
					"style":     2,
					"label":     "JSON",
					"value":     a.InputJSON,
					"required":  true,
				},
			}},
		},
	}
}

func (d *Daemon) runPushoverNotifier(ctx context.Context, c *pushover.Client) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		d.sendPushoverApproval(ctx, c, a)
	})
}

func (d *Daemon) sendPushoverApproval(ctx context.Context, c *pushover.Client, a *approval.Approval) {
	resp, err := c.Send(ctx, pushover.MessageOptions{Title: "Onibi approval", Message: formatApprovalWithPolicy(a, d.providerOutputPolicy("notify")), Priority: 2, Retry: 30 * time.Second, Expire: time.Hour})
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
			if err := d.decideProviderApproval(ctx, a.ID, approval.VerdictApprove, 0); err != nil {
				switch {
				case errors.Is(err, approval.ErrAlreadyDecided):
					d.audit(ctx, "notify.pushover.approve_already_decided", a.SessionID, "", 0, "approval="+a.ID+" receipt="+resp.Receipt)
				case errors.Is(err, approval.ErrExpired):
					d.audit(ctx, "notify.pushover.approve_expired", a.SessionID, "", 0, "approval="+a.ID+" receipt="+resp.Receipt)
				default:
					d.audit(ctx, "notify.pushover.approve_error", a.SessionID, "", 0, "approval="+a.ID+" receipt="+resp.Receipt+" err="+err.Error())
				}
			} else {
				d.audit(ctx, "notify.pushover.approve", a.SessionID, "", 0, "approval="+a.ID+" receipt="+resp.Receipt)
			}
		} else if got.Expired == 1 {
			state = "expired"
		}
		d.audit(ctx, "notify.pushover.receipt."+state, a.SessionID, "", 0, "approval="+a.ID+" receipt="+resp.Receipt)
	}()
}

func (d *Daemon) runNtfyNotifier(ctx context.Context, c *ntfy.Client) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		if err := c.Publish(ctx, ntfy.Message{Title: "Onibi approval", Body: formatApprovalWithPolicy(a, d.providerOutputPolicy("notify")), Tags: "warning"}); err != nil {
			d.audit(ctx, "notify.ntfy.error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
			return
		}
		d.audit(ctx, "notify.ntfy.sent", a.SessionID, "", 0, "approval="+a.ID)
	})
}

func (d *Daemon) runGotifyNotifier(ctx context.Context, c *gotify.Client) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		if err := c.Send(ctx, gotify.Message{Title: "Onibi approval", Message: formatApprovalWithPolicy(a, d.providerOutputPolicy("notify")), Priority: 8}); err != nil {
			d.audit(ctx, "notify.gotify.error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
			return
		}
		d.audit(ctx, "notify.gotify.sent", a.SessionID, "", 0, "approval="+a.ID)
	})
}

func (d *Daemon) runWebPushNotifier(ctx context.Context) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		web.SendApprovalPushNotifications(ctx, d.DB, a, d.Log)
	})
}

func (d *Daemon) forwardNotifyApprovals(ctx context.Context, send func(*approval.Approval)) {
	if d.Queue == nil {
		return
	}
	events, unsub, err := d.Queue.Subscribe()
	if err != nil {
		if d.Log != nil {
			d.Log.Warn("notify approval subscribe failed", "err", err)
		}
		return
	}
	defer unsub()
	sent := map[string]bool{}
	sendOnce := func(a *approval.Approval) {
		if a == nil || sent[a.ID] {
			return
		}
		sent[a.ID] = true
		send(a)
	}
	pending, err := d.Queue.Pending(ctx)
	if err != nil {
		d.Log.Warn("notify approval replay failed", slog.Any("err", err))
	} else {
		for _, a := range pending {
			select {
			case <-ctx.Done():
				return
			default:
				sendOnce(a)
			}
		}
	}
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
				sendOnce(&a)
			}
		}
	}
}

func (d *Daemon) handleProviderText(ctx context.Context, target, text string, actor int64) (string, error) {
	return d.handleProviderTextFor(ctx, target, text, actor, "")
}

func (d *Daemon) handleProviderTextFor(ctx context.Context, target, text string, actor int64, provider string) (string, error) {
	if handled, reply := d.handleProviderTextCommand(ctx, text, actor); handled {
		return d.prepareProviderOutputFor(provider, reply), nil
	}
	out, err := d.SendSessionTextAndCapture(ctx, target, text, true)
	return d.prepareProviderOutputFor(provider, out), err
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
