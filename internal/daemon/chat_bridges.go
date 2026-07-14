//go:build !onibi_remote

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/apns"
	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/chatout"
	"github.com/gongahkia/onibi/internal/discord"
	emailapi "github.com/gongahkia/onibi/internal/email"
	"github.com/gongahkia/onibi/internal/gotify"
	"github.com/gongahkia/onibi/internal/irc"
	"github.com/gongahkia/onibi/internal/matrix"
	"github.com/gongahkia/onibi/internal/ntfy"
	"github.com/gongahkia/onibi/internal/pushover"
	signalapi "github.com/gongahkia/onibi/internal/signal"
	"github.com/gongahkia/onibi/internal/slack"
	"github.com/gongahkia/onibi/internal/sms"
	"github.com/gongahkia/onibi/internal/web"
	"github.com/gongahkia/onibi/internal/zulip"
)

const (
	matrixKVSince          = "matrix.since"
	matrixKVApprovalPrefix = "matrix.approval.event."
	matrixDefaultDeviceID  = "ONIBI"
	matrixOneTimeKeyCount  = 10
)

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

const (
	zulipDefaultTopicPrefix = "onibi-"
	zulipMessageChunkLimit  = 3800
	zulipReconnectMaxWait   = 30 * time.Second
)

func (d *Daemon) runMatrixBridge(ctx context.Context, c *matrix.Client) error {
	if c == nil {
		return errors.New("matrix client nil")
	}
	if strings.TrimSpace(d.Matrix.RoomID) == "" {
		return errors.New("matrix room id required")
	}
	who, err := c.CheckRoomOwner(ctx, d.Matrix.RoomID, 50)
	if err != nil {
		return err
	}
	encrypted, err := c.IsEncryptedRoom(ctx, d.Matrix.RoomID)
	if err != nil {
		return err
	}
	d.setMatrixEncryptedRoom(encrypted)
	if encrypted && !d.Matrix.AllowEncrypted {
		return errors.New("matrix encrypted rooms require ONIBI_MATRIX_ALLOW_ENCRYPTED=1")
	}
	if encrypted && (strings.TrimSpace(d.Matrix.OwnerUserID) == "" || strings.TrimSpace(d.Matrix.OwnerDeviceID) == "") {
		return errors.New("matrix encrypted rooms require ONIBI_MATRIX_OWNER_USER_ID and ONIBI_MATRIX_OWNER_DEVICE_ID")
	}
	cryptoState, pickleKey, cryptoReady, err := d.ensureMatrixCryptoState(ctx, c, who)
	if err != nil {
		return err
	}
	if encrypted && !cryptoReady {
		return errors.New("matrix encrypted rooms require an encrypted local crypto state store")
	}
	if encrypted && cryptoReady {
		cryptoState, err = d.ensureMatrixTrustedOwnerDevice(ctx, c, cryptoState)
		if err != nil {
			return err
		}
		if err := d.ensureMatrixRoomKeyShared(ctx, c, cryptoState, pickleKey, d.Matrix.OwnerUserID, d.Matrix.OwnerDeviceID); err != nil {
			return err
		}
		d.audit(ctx, "provider.matrix.e2ee_enabled", "", "", 0, "room="+d.Matrix.RoomID+" owner_device="+d.Matrix.OwnerDeviceID)
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
		if encrypted {
			for _, ev := range sync.ToDevice.Events {
				if err := d.handleMatrixToDeviceEvent(ctx, ev); err != nil {
					d.audit(ctx, "provider.matrix.to_device_error", "", "", 0, "type="+ev.Type+" err="+err.Error())
				}
			}
		}
		room := sync.Rooms.Join[d.Matrix.RoomID]
		for _, ev := range room.Timeline.Events {
			if encrypted && ev.Type == matrix.EventRoomEncrypted {
				var err error
				ev, err = d.decryptMatrixRoomEvent(ctx, ev)
				if err != nil {
					d.audit(ctx, "provider.matrix.decrypt_error", "", "", 0, "event_id="+ev.EventID+" err="+err.Error())
					continue
				}
			}
			d.handleMatrixEvent(ctx, c, ev)
		}
		if sync.NextBatch != "" {
			since = sync.NextBatch
			if d.DB != nil {
				_ = d.DB.KVSetString(ctx, matrixKVSince, since)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func (d *Daemon) ensureMatrixCryptoState(ctx context.Context, c *matrix.Client, who matrix.WhoAmI) (matrix.CryptoState, []byte, bool, error) {
	if d == nil || d.DB == nil || d.DB.CryptBox() == nil {
		return matrix.CryptoState{}, nil, false, nil
	}
	deviceID := strings.TrimSpace(who.DeviceID)
	if deviceID == "" {
		deviceID = matrixDefaultDeviceID
	}
	state, pickleKey, created, err := matrix.EnsureCryptoState(ctx, d.DB, who.UserID, deviceID, matrixOneTimeKeyCount)
	if err != nil {
		return matrix.CryptoState{}, nil, false, err
	}
	otks, err := matrix.OlmAccountOneTimeKeys(state, pickleKey)
	if err != nil {
		return matrix.CryptoState{}, nil, false, err
	}
	uploadDeviceKeys := !state.AccountShared || created
	if !uploadDeviceKeys && len(otks) == 0 {
		return state, pickleKey, true, nil
	}
	next, resp, err := c.UploadCryptoKeys(ctx, state, pickleKey, uploadDeviceKeys)
	if err != nil {
		return matrix.CryptoState{}, nil, false, err
	}
	if err := matrix.SaveCryptoState(ctx, d.DB, next); err != nil {
		return matrix.CryptoState{}, nil, false, err
	}
	d.audit(ctx, "provider.matrix.crypto_upload", "", "", 0, fmt.Sprintf("device_id=%s device_keys=%t one_time_keys=%d", next.DeviceID, uploadDeviceKeys, resp.OneTimeKeyCounts[matrix.KeyAlgorithmSignedCurve255]))
	return next, pickleKey, true, nil
}

func (d *Daemon) ensureMatrixTrustedOwnerDevice(ctx context.Context, c *matrix.Client, state matrix.CryptoState) (matrix.CryptoState, error) {
	query, err := c.QueryKeys(ctx, map[string][]string{d.Matrix.OwnerUserID: {d.Matrix.OwnerDeviceID}}, 10*time.Second)
	if err != nil {
		return state, err
	}
	keys, err := matrix.DeviceKeysFromQuery(query, d.Matrix.OwnerUserID, d.Matrix.OwnerDeviceID)
	if err != nil {
		return state, err
	}
	if _, ok := matrix.TrustedDeviceKeyFor(state, d.Matrix.OwnerUserID, d.Matrix.OwnerDeviceID); !ok && !d.Matrix.SASVerified {
		return state, errors.New("matrix encrypted rooms require ONIBI_MATRIX_SAS_VERIFIED=1 after manual SAS comparison before first owner-device pin")
	}
	next, err := matrix.PinTrustedDevice(state, keys)
	if err != nil {
		return state, err
	}
	if err := matrix.SaveCryptoState(ctx, d.DB, next); err != nil {
		return state, err
	}
	d.audit(ctx, "provider.matrix.owner_device_pinned", "", "", 0, "user="+d.Matrix.OwnerUserID+" device="+d.Matrix.OwnerDeviceID)
	return next, nil
}

func (d *Daemon) ensureMatrixRoomKeyShared(ctx context.Context, c *matrix.Client, state matrix.CryptoState, pickleKey []byte, userID, deviceID string) error {
	if d == nil || d.DB == nil {
		return nil
	}
	if state.MegolmOutboundSessions == nil {
		state.MegolmOutboundSessions = map[string]matrix.MegolmOutboundState{}
	}
	outbound, ok := state.MegolmOutboundSessions[d.Matrix.RoomID]
	var roomKey matrix.RoomKeyContent
	var err error
	if ok {
		roomKey, err = matrix.RoomKeyFromMegolmOutboundState(outbound, pickleKey)
	} else {
		outbound, roomKey, err = matrix.NewMegolmOutboundState(d.Matrix.RoomID, pickleKey)
	}
	if err != nil {
		return err
	}
	if !matrix.IsDeviceTrusted(state, userID, deviceID) {
		return errors.New("matrix room key share requires a pinned SAS-verified owner device")
	}
	state, outbound, err = c.ShareRoomKeyWithDevices(ctx, state, outbound, roomKey, pickleKey, []matrix.RoomKeyShareTarget{{UserID: userID, DeviceID: deviceID}}, 10*time.Second)
	if err != nil {
		return err
	}
	state.MegolmOutboundSessions[d.Matrix.RoomID] = outbound
	if err := matrix.SaveCryptoState(ctx, d.DB, state); err != nil {
		return err
	}
	shared := 0
	for _, devices := range outbound.SharedWith {
		shared += len(devices)
	}
	d.audit(ctx, "provider.matrix.room_key_shared", "", "", 0, fmt.Sprintf("room=%s users=1 devices=%d", d.Matrix.RoomID, shared))
	return nil
}

func (d *Daemon) handleMatrixToDeviceEvent(ctx context.Context, ev matrix.Event) error {
	if ev.Type != matrix.EventRoomEncrypted || ev.Sender != d.Matrix.OwnerUserID {
		return nil
	}
	if d.DB == nil {
		return errors.New("matrix encrypted to-device event requires crypto state db")
	}
	state, ok, err := matrix.LoadCryptoState(ctx, d.DB)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("matrix encrypted to-device event requires initialized crypto state")
	}
	pickleKey, err := state.PickleKeyBytes()
	if err != nil {
		return err
	}
	trusted, ok := matrix.TrustedDeviceKeyFor(state, d.Matrix.OwnerUserID, d.Matrix.OwnerDeviceID)
	if !ok {
		return errors.New("matrix encrypted to-device event requires pinned owner device")
	}
	var content matrix.OlmEncryptedContent
	if err := json.Unmarshal(ev.Content, &content); err != nil {
		return err
	}
	next, session, payload, err := matrix.DecryptOlmToDevice(state, pickleKey, content)
	if err != nil {
		return err
	}
	if !matrix.IsTrustedDeviceEvent(next, d.Matrix.OwnerUserID, d.Matrix.OwnerDeviceID, content.SenderKey, payload.Keys["ed25519"]) || trusted.Curve25519Key != content.SenderKey {
		return errors.New("matrix encrypted to-device event sender is not the pinned owner device")
	}
	roomKey, err := matrix.ValidateOlmRoomKeyPayload(payload, ev.Sender, next, trusted)
	if err != nil {
		return err
	}
	if roomKey.RoomID != d.Matrix.RoomID {
		return errors.New("matrix encrypted to-device room key does not match configured room")
	}
	session.UserID = d.Matrix.OwnerUserID
	session.DeviceID = d.Matrix.OwnerDeviceID
	if next.OlmSessions == nil {
		next.OlmSessions = map[string]matrix.OlmSessionState{}
	}
	next.OlmSessions[matrix.OlmSessionKey(session.UserID, session.DeviceID, session.SessionID)] = session
	inbound, err := matrix.NewMegolmInboundState(roomKey, trusted.Curve25519Key, pickleKey)
	if err != nil {
		return err
	}
	next, err = matrix.StoreMegolmInboundSession(next, inbound)
	if err != nil {
		return err
	}
	if err := matrix.SaveCryptoState(ctx, d.DB, next); err != nil {
		return err
	}
	d.audit(ctx, "provider.matrix.room_key_received", "", "", 0, "room="+roomKey.RoomID+" session="+roomKey.SessionID+" owner_device="+d.Matrix.OwnerDeviceID)
	return nil
}

func (d *Daemon) decryptMatrixRoomEvent(ctx context.Context, ev matrix.Event) (matrix.Event, error) {
	if d.DB == nil {
		return matrix.Event{}, errors.New("matrix encrypted room event requires crypto state db")
	}
	state, ok, err := matrix.LoadCryptoState(ctx, d.DB)
	if err != nil {
		return matrix.Event{}, err
	}
	if !ok {
		return matrix.Event{}, errors.New("matrix encrypted room event requires initialized crypto state")
	}
	pickleKey, err := state.PickleKeyBytes()
	if err != nil {
		return matrix.Event{}, err
	}
	trusted, ok := matrix.TrustedDeviceKeyFor(state, d.Matrix.OwnerUserID, d.Matrix.OwnerDeviceID)
	if !ok {
		return matrix.Event{}, errors.New("matrix encrypted room event requires pinned owner device")
	}
	var content matrix.MegolmEncryptedContent
	if err := json.Unmarshal(ev.Content, &content); err != nil {
		return matrix.Event{}, err
	}
	if content.Algorithm != matrix.AlgorithmMegolmV1 || content.SenderKey != trusted.Curve25519Key {
		return matrix.Event{}, errors.New("matrix encrypted room event sender is not the pinned owner device")
	}
	key, inbound, ok := matrix.MegolmInboundSessionFor(state, content.SenderKey, content.SessionID)
	if !ok {
		return matrix.Event{}, errors.New("matrix encrypted room event has no trusted inbound session")
	}
	nextInbound, payload, _, err := matrix.DecryptMegolmRoomEvent(inbound, pickleKey, content, d.Matrix.RoomID)
	if err != nil {
		return matrix.Event{}, err
	}
	if payload.Type == matrix.EventRoomEncrypted {
		return matrix.Event{}, errors.New("matrix encrypted room event nested encryption rejected")
	}
	state.MegolmInboundSessions[key] = nextInbound
	if err := matrix.SaveCryptoState(ctx, d.DB, state); err != nil {
		return matrix.Event{}, err
	}
	d.audit(ctx, "provider.matrix.decrypt", "", "", 0, "event_id="+ev.EventID+" session="+content.SessionID+" type="+payload.Type)
	return matrix.Event{EventID: ev.EventID, Type: payload.Type, Sender: ev.Sender, Content: payload.Content}, nil
}

func (d *Daemon) setMatrixEncryptedRoom(encrypted bool) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.matrixEncryptedRoom = encrypted
	d.mu.Unlock()
}

func (d *Daemon) isMatrixEncryptedRoom() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.matrixEncryptedRoom
}

func (d *Daemon) sendMatrixTextEvents(ctx context.Context, c *matrix.Client, sessionID, text string) ([]matrix.SentEvent, error) {
	if d == nil {
		return nil, errors.New("daemon nil")
	}
	if c == nil {
		return nil, errors.New("matrix client nil")
	}
	if !d.isMatrixEncryptedRoom() {
		return c.SendTextEventChunks(ctx, d.Matrix.RoomID, text)
	}
	state, pickleKey, outbound, senderKey, err := d.matrixOutboundCrypto(ctx)
	if err != nil {
		return nil, err
	}
	var events []matrix.SentEvent
	if err := chatout.SendChunks(ctx, text, matrix.MessageChunkLimit, 0, c.Sleep, func(ctx context.Context, chunk string) error {
		var encrypted matrix.MegolmEncryptedContent
		outbound, encrypted, err = matrix.EncryptMegolmRoomEvent(outbound, pickleKey, senderKey, state.DeviceID, d.Matrix.RoomID, "m.room.message", matrix.RoomMessage{MsgType: "m.text", Body: chunk})
		if err != nil {
			return err
		}
		eventID, err := c.SendMegolmEncryptedEvent(ctx, d.Matrix.RoomID, encrypted)
		if err != nil {
			return err
		}
		events = append(events, matrix.SentEvent{EventID: eventID, Body: chunk})
		state.MegolmOutboundSessions[d.Matrix.RoomID] = outbound
		return matrix.SaveCryptoState(ctx, d.DB, state)
	}); err != nil {
		return nil, err
	}
	return events, nil
}

func (d *Daemon) matrixOutboundCrypto(ctx context.Context) (matrix.CryptoState, []byte, matrix.MegolmOutboundState, string, error) {
	if d == nil || d.DB == nil {
		return matrix.CryptoState{}, nil, matrix.MegolmOutboundState{}, "", errors.New("matrix encrypted send requires crypto state db")
	}
	state, ok, err := matrix.LoadCryptoState(ctx, d.DB)
	if err != nil {
		return matrix.CryptoState{}, nil, matrix.MegolmOutboundState{}, "", err
	}
	if !ok {
		return matrix.CryptoState{}, nil, matrix.MegolmOutboundState{}, "", errors.New("matrix encrypted send requires initialized crypto state")
	}
	pickleKey, err := state.PickleKeyBytes()
	if err != nil {
		return matrix.CryptoState{}, nil, matrix.MegolmOutboundState{}, "", err
	}
	if state.DeviceKeys == nil {
		return matrix.CryptoState{}, nil, matrix.MegolmOutboundState{}, "", errors.New("matrix encrypted send requires device keys")
	}
	senderKey := strings.TrimSpace(state.DeviceKeys.Keys["curve25519:"+state.DeviceID])
	if senderKey == "" {
		return matrix.CryptoState{}, nil, matrix.MegolmOutboundState{}, "", errors.New("matrix encrypted send requires sender curve25519 key")
	}
	outbound, ok := state.MegolmOutboundSessions[d.Matrix.RoomID]
	if !ok || strings.TrimSpace(outbound.Pickle) == "" {
		return matrix.CryptoState{}, nil, matrix.MegolmOutboundState{}, "", errors.New("matrix encrypted send requires initialized outbound Megolm state")
	}
	return state, pickleKey, outbound, senderKey, nil
}

func (d *Daemon) handleMatrixEvent(ctx context.Context, c *matrix.Client, ev matrix.Event) {
	if d.Matrix.OwnerUserID != "" && ev.Sender != d.Matrix.OwnerUserID {
		return
	}
	if eventID, key, ok := matrix.Reaction(ev); ok {
		d.handleMatrixReaction(ctx, c, ev, eventID, key)
		return
	}
	body := matrix.MessageBody(ev)
	if body == "" {
		return
	}
	sessionID := d.providerTargetSessionID("")
	d.audit(ctx, "provider.matrix.text_in", sessionID, body, 0, "event_id="+ev.EventID+" sender="+ev.Sender)
	out, err := d.handleProviderTextFor(ctx, "", body, 0, "matrix")
	if err != nil {
		_, _ = d.sendMatrixTextEvents(ctx, c, sessionID, "Input failed: "+err.Error())
		return
	}
	d.postMatrixTail(ctx, c, sessionID, out)
}

func (d *Daemon) handleMatrixReaction(ctx context.Context, c *matrix.Client, ev matrix.Event, eventID, key string) {
	id := d.matrixApprovalForEvent(ctx, eventID)
	if id == "" {
		return
	}
	verdict := matrixReactionVerdict(key)
	if verdict == "" {
		return
	}
	sessionID := d.approvalSessionID(ctx, id)
	d.audit(ctx, "provider.matrix.reaction", sessionID, key, 0, "event_id="+eventID+" reaction_event="+ev.EventID+" approval="+id+" sender="+ev.Sender)
	text := d.handleMatrixApprovalDecision(ctx, id, verdict)
	_, _ = d.sendMatrixTextEvents(ctx, c, sessionID, text)
}

func (d *Daemon) handleMatrixApprovalDecision(ctx context.Context, id string, verdict approval.Verdict) string {
	if id == "" || verdict == "" {
		return "Approval decision failed: invalid Matrix reaction."
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

func (d *Daemon) postMatrixTail(ctx context.Context, c *matrix.Client, sessionID, text string) {
	events, err := d.sendMatrixTextEvents(ctx, c, sessionID, text)
	if err != nil {
		d.audit(ctx, "provider.matrix.tail_error", sessionID, "", 0, "err="+err.Error())
		return
	}
	for i, ev := range events {
		d.audit(ctx, "provider.matrix.tail_chunk", sessionID, ev.Body, 0, fmt.Sprintf("event_id=%s index=%d bytes=%d", ev.EventID, i, len(ev.Body)))
	}
}

func (d *Daemon) forwardApprovalsToMatrix(ctx context.Context, c *matrix.Client) {
	if d.Queue == nil {
		return
	}
	sent := map[string]bool{}
	send := func(a *approval.Approval) {
		if a == nil || sent[a.ID] {
			return
		}
		sent[a.ID] = true
		text := formatApprovalWithPolicy(a, d.providerOutputPolicy("matrix")) + "\nReact ✅ to approve or ❌ to deny."
		events, err := d.sendMatrixTextEvents(ctx, c, a.SessionID, text)
		if err != nil {
			d.audit(ctx, "provider.matrix.approval_error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
			return
		}
		eventIDs := make([]string, 0, len(events))
		for _, ev := range events {
			if ev.EventID == "" {
				continue
			}
			eventIDs = append(eventIDs, ev.EventID)
			d.storeMatrixApprovalEvent(ctx, ev.EventID, a.ID)
		}
		d.audit(ctx, "provider.matrix.approval_sent", a.SessionID, text, 0, "approval="+a.ID+" event_ids="+strings.Join(eventIDs, ","))
	}
	events, unsub, err := d.Queue.Subscribe()
	if err != nil {
		if d.Log != nil {
			d.Log.Warn("matrix approval subscribe failed", "err", err)
		}
		return
	}
	defer unsub()
	if pending, err := d.Queue.Pending(ctx); err == nil {
		for _, a := range pending {
			send(a)
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
				send(&a)
			}
		}
	}
}

func (d *Daemon) storeMatrixApprovalEvent(ctx context.Context, eventID, approvalID string) {
	if d.DB == nil || strings.TrimSpace(eventID) == "" || strings.TrimSpace(approvalID) == "" {
		return
	}
	_ = d.DB.KVSetString(ctx, matrixKVApprovalPrefix+eventID, approvalID)
}

func (d *Daemon) matrixApprovalForEvent(ctx context.Context, eventID string) string {
	if d.DB == nil || strings.TrimSpace(eventID) == "" {
		return ""
	}
	id, _, err := d.DB.KVGetString(ctx, matrixKVApprovalPrefix+eventID)
	if err != nil {
		return ""
	}
	return id
}

func matrixReactionVerdict(key string) approval.Verdict {
	switch strings.TrimSpace(key) {
	case "✅", "👍":
		return approval.VerdictApprove
	case "❌", "👎":
		return approval.VerdictDeny
	default:
		return ""
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
	if _, err := d.Queue.DecideIdempotently(ctx, id, approval.VerdictEdit, edited, "provider edit", 0); err != nil {
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
	if _, err := d.Queue.DecideIdempotently(ctx, id, approval.VerdictEdit, edited, "provider edit", 0); err != nil {
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

func (d *Daemon) runZulipBridge(ctx context.Context, c *zulip.Client) error {
	if c == nil {
		return errors.New("zulip client nil")
	}
	if strings.TrimSpace(d.Zulip.Stream) == "" {
		return errors.New("zulip stream required")
	}
	go d.forwardApprovalsToZulip(ctx, c)
	return c.TailEvents(ctx, zulip.TailOptions{
		QueueOptions: zulip.QueueOptions{
			EventTypes: []string{"message"},
			Narrow:     [][]string{{"channel", d.Zulip.Stream}},
		},
		RetryMin: time.Second,
		RetryMax: zulipReconnectMaxWait,
		AfterError: func(err error, delay time.Duration, attempt int) {
			d.audit(ctx, "provider.zulip.reconnect", "", "", 0, fmt.Sprintf("attempt=%d delay=%s err=%s", attempt, delay, err))
		},
	}, func(ev zulip.Event) error {
		return d.handleZulipEvent(ctx, c, ev)
	})
}

func (d *Daemon) handleZulipEvent(ctx context.Context, c *zulip.Client, ev zulip.Event) error {
	if ev.Type != "message" || ev.Message == nil {
		return nil
	}
	msg := ev.Message
	if msg.Type != "" && msg.Type != "stream" && msg.Type != "channel" {
		return nil
	}
	if owner := strings.TrimSpace(d.Zulip.OwnerEmail); owner != "" && !strings.EqualFold(owner, msg.SenderEmail) {
		return nil
	}
	if bot := strings.TrimSpace(d.Zulip.Email); bot != "" && strings.EqualFold(bot, msg.SenderEmail) {
		return nil
	}
	topic := msg.Topic()
	sessionID := d.zulipSessionFromTopic(topic)
	if sessionID == "" {
		return nil
	}
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return nil
	}
	d.audit(ctx, "provider.zulip.text_in", sessionID, text, 0, "stream="+d.Zulip.Stream+" topic="+topic+" sender="+msg.SenderEmail)
	out, err := d.handleProviderTextFor(ctx, sessionID, text, 0, "zulip")
	if err != nil {
		_, _ = c.SendStreamMessage(ctx, zulip.StreamMessage{Stream: d.Zulip.Stream, Topic: topic, Content: "Input failed: " + err.Error()})
		return nil
	}
	d.postZulipTail(ctx, c, topic, sessionID, out)
	return nil
}

func (d *Daemon) forwardApprovalsToZulip(ctx context.Context, c *zulip.Client) {
	if d.Queue == nil || strings.TrimSpace(d.Zulip.Stream) == "" {
		return
	}
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		topic := d.zulipTopicForSession(a.SessionID)
		text := formatApprovalWithPolicy(a, d.providerOutputPolicy("zulip")) + "\n\nReply `approve " + a.ID + "` or `deny " + a.ID + "` in this topic."
		resp, err := c.SendStreamMessage(ctx, zulip.StreamMessage{Stream: d.Zulip.Stream, Topic: topic, Content: text})
		if err != nil {
			d.audit(ctx, "provider.zulip.approval_error", a.SessionID, "", 0, "approval="+a.ID+" topic="+topic+" err="+err.Error())
			return
		}
		d.audit(ctx, "provider.zulip.approval_sent", a.SessionID, "", 0, fmt.Sprintf("approval=%s topic=%s message=%d", a.ID, topic, resp.ID))
	})
}

func (d *Daemon) postZulipTail(ctx context.Context, c *zulip.Client, topic, sessionID, text string) {
	if c == nil {
		return
	}
	for i, chunk := range chatout.Chunks(text, zulipMessageChunkLimit) {
		if _, err := c.SendStreamMessage(ctx, zulip.StreamMessage{Stream: d.Zulip.Stream, Topic: topic, Content: chunk}); err != nil {
			d.audit(ctx, "provider.zulip.tail_error", sessionID, "", 0, "topic="+topic+" err="+err.Error())
			return
		}
		d.audit(ctx, "provider.zulip.tail_chunk", sessionID, chunk, 0, fmt.Sprintf("topic=%s index=%d bytes=%d", topic, i, len(chunk)))
	}
}

func (d *Daemon) zulipTopicForSession(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = "default"
	}
	return d.zulipTopicPrefix() + sessionID
}

func (d *Daemon) zulipSessionFromTopic(topic string) string {
	topic = strings.TrimSpace(topic)
	prefix := d.zulipTopicPrefix()
	sessionID, ok := strings.CutPrefix(topic, prefix)
	if !ok {
		return ""
	}
	return strings.TrimSpace(sessionID)
}

func (d *Daemon) zulipTopicPrefix() string {
	prefix := strings.TrimSpace(d.Zulip.TopicPrefix)
	if prefix == "" {
		return zulipDefaultTopicPrefix
	}
	return prefix
}

func (d *Daemon) runIRCBridge(ctx context.Context, c *irc.Client) error {
	if c == nil {
		return errors.New("irc client nil")
	}
	if strings.TrimSpace(d.IRC.OwnerNick) == "" {
		return errors.New("irc owner nick required")
	}
	prev := c.AfterError
	c.AfterError = func(err error, delay time.Duration, attempt int) {
		d.audit(ctx, "provider.irc.reconnect", "", "", 0, fmt.Sprintf("attempt=%d delay=%s err=%v", attempt, delay, err))
		if prev != nil {
			prev(err, delay, attempt)
		}
	}
	go d.forwardApprovalsToIRC(ctx, c)
	return c.RunWithReconnect(ctx, func(msg irc.Message) error {
		return d.handleIRCMessage(ctx, c, msg)
	})
}

func (d *Daemon) handleIRCMessage(ctx context.Context, c *irc.Client, msg irc.Message) error {
	if msg.Command != "PRIVMSG" || len(msg.Params) == 0 {
		return nil
	}
	if !strings.EqualFold(msg.Params[0], d.IRC.Nick) {
		return nil
	}
	from := msg.Nick()
	if !strings.EqualFold(from, d.IRC.OwnerNick) {
		return nil
	}
	text := strings.TrimSpace(msg.Trailing)
	if text == "" {
		return nil
	}
	if action, id, ok := irc.ParseOnibiCommand(text); ok {
		reply := d.handleIRCApprovalCommand(ctx, action, id)
		_ = c.SendPrivmsg(ctx, from, reply)
		return nil
	}
	sessionID := d.providerTargetSessionID("")
	d.audit(ctx, "provider.irc.text_in", sessionID, text, 0, "nick="+from)
	out, err := d.handleProviderTextFor(ctx, "", text, 0, "irc")
	if err != nil {
		_ = c.SendPrivmsg(ctx, from, "Input failed: "+err.Error())
		return nil
	}
	d.postIRCTail(ctx, c, from, sessionID, out)
	return nil
}

func (d *Daemon) handleIRCApprovalCommand(ctx context.Context, action, id string) string {
	verdict := approvalVerdictForAction(action)
	sessionID := d.approvalSessionID(ctx, id)
	d.audit(ctx, "provider.irc.command", sessionID, irc.FormatOnibiCommand(action, id), 0, "action="+action+" approval="+id+" nick="+d.IRC.OwnerNick)
	if verdict == "" {
		return "Approval " + id + " failed: invalid action."
	}
	if err := d.decideProviderApproval(ctx, id, verdict, 0); err != nil {
		return fmt.Sprintf("Approval %s failed: %v", id, err)
	}
	return fmt.Sprintf("Approval %s %s.", id, verdict)
}

func (d *Daemon) forwardApprovalsToIRC(ctx context.Context, c *irc.Client) {
	if d.Queue == nil || strings.TrimSpace(d.IRC.OwnerNick) == "" {
		return
	}
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		text := formatApprovalWithPolicy(a, d.providerOutputPolicy("irc")) + "\nReply " + irc.FormatOnibiCommand("approve", a.ID) + " or " + irc.FormatOnibiCommand("deny", a.ID) + "."
		if err := c.SendPrivmsg(ctx, d.IRC.OwnerNick, text); err != nil {
			d.audit(ctx, "provider.irc.approval_error", a.SessionID, "", 0, "approval="+a.ID+" nick="+d.IRC.OwnerNick+" err="+err.Error())
			return
		}
		d.audit(ctx, "provider.irc.approval_sent", a.SessionID, "", 0, "approval="+a.ID+" nick="+d.IRC.OwnerNick)
	})
}

func (d *Daemon) postIRCTail(ctx context.Context, c *irc.Client, nick, sessionID, text string) {
	if c == nil {
		return
	}
	for i, chunk := range chatout.Chunks(text, irc.MessageChunkLimit) {
		if err := c.SendPrivmsg(ctx, nick, chunk); err != nil {
			d.audit(ctx, "provider.irc.tail_error", sessionID, "", 0, "nick="+nick+" err="+err.Error())
			return
		}
		d.audit(ctx, "provider.irc.tail_chunk", sessionID, chunk, 0, fmt.Sprintf("nick=%s index=%d bytes=%d", nick, i, len(chunk)))
	}
}

func (d *Daemon) runSignalBridge(ctx context.Context, c *signalapi.Client) error {
	if c == nil {
		return errors.New("signal client nil")
	}
	if !d.signalHasTarget() {
		return errors.New("signal recipient or group required")
	}
	if err := c.Check(ctx); err != nil {
		return err
	}
	if _, err := c.SubscribeReceive(ctx); err != nil {
		return err
	}
	go d.forwardApprovalsToSignal(ctx, c)
	return c.TailEvents(ctx, signalapi.TailOptions{RetryMin: time.Second, RetryMax: 30 * time.Second}, func(ev signalapi.Event) error {
		return d.handleSignalEvent(ctx, c, ev)
	})
}

func (d *Daemon) handleSignalEvent(ctx context.Context, c *signalapi.Client, ev signalapi.Event) error {
	if !d.signalAllowedSource(ev.Envelope) {
		return nil
	}
	msg := ev.Envelope.DataMessage
	if msg == nil {
		return nil
	}
	if verdict, id, ok := d.signalReactionDecision(msg.Reaction); ok {
		sessionID := d.approvalSessionID(ctx, id)
		d.audit(ctx, "provider.signal.reaction", sessionID, "", 0, "approval="+id+" verdict="+string(verdict)+" source="+d.signalSource(ev.Envelope))
		if err := d.decideProviderApproval(ctx, id, verdict, 0); err != nil {
			_ = d.sendSignalText(ctx, c, "Approval "+id+" failed: "+err.Error())
			return nil
		}
		_ = d.sendSignalText(ctx, c, "Approval "+id+" "+string(verdict)+".")
		return nil
	}
	text := strings.TrimSpace(msg.Message)
	if text == "" {
		return nil
	}
	sessionID := d.providerTargetSessionID("")
	d.audit(ctx, "provider.signal.text_in", sessionID, text, 0, "source="+d.signalSource(ev.Envelope))
	out, err := d.handleProviderTextFor(ctx, "", text, 0, "signal")
	if err != nil {
		_ = d.sendSignalText(ctx, c, "Input failed: "+err.Error())
		return nil
	}
	d.postSignalTail(ctx, c, sessionID, out)
	return nil
}

func (d *Daemon) forwardApprovalsToSignal(ctx context.Context, c *signalapi.Client) {
	if d.Queue == nil || !d.signalHasTarget() {
		return
	}
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		text := formatApprovalWithPolicy(a, d.providerOutputPolicy("signal")) + "\n\nReact \U0001f44d to approve or \U0001f44e to deny."
		sent, err := c.Send(ctx, signalapi.SendRequest{Recipients: d.Signal.Recipients, GroupID: d.Signal.GroupID, Message: text})
		if err != nil {
			d.audit(ctx, "provider.signal.approval_error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
			return
		}
		d.signalMu.Lock()
		d.signalApprovals[sent.Timestamp] = a.ID
		d.signalMu.Unlock()
		d.audit(ctx, "provider.signal.approval_sent", a.SessionID, "", 0, fmt.Sprintf("approval=%s timestamp=%d", a.ID, sent.Timestamp))
	})
}

func (d *Daemon) postSignalTail(ctx context.Context, c *signalapi.Client, sessionID, text string) {
	for i, chunk := range chatout.Chunks(text, 3800) {
		if err := d.sendSignalText(ctx, c, chunk); err != nil {
			d.audit(ctx, "provider.signal.tail_error", sessionID, "", 0, "err="+err.Error())
			return
		}
		d.audit(ctx, "provider.signal.tail_chunk", sessionID, chunk, 0, fmt.Sprintf("index=%d bytes=%d", i, len(chunk)))
	}
}

func (d *Daemon) sendSignalText(ctx context.Context, c *signalapi.Client, text string) error {
	_, err := c.Send(ctx, signalapi.SendRequest{Recipients: d.Signal.Recipients, GroupID: d.Signal.GroupID, Message: d.prepareProviderOutputFor("signal", text)})
	return err
}

func (d *Daemon) signalHasTarget() bool {
	return len(d.Signal.Recipients) > 0 || strings.TrimSpace(d.Signal.GroupID) != ""
}

func (d *Daemon) signalAllowedSource(env signalapi.Envelope) bool {
	want := strings.TrimSpace(d.Signal.Owner)
	if want == "" && strings.TrimSpace(d.Signal.GroupID) == "" && len(d.Signal.Recipients) == 1 {
		want = d.Signal.Recipients[0]
	}
	if want == "" {
		return true
	}
	for _, got := range []string{env.Source, env.SourceNumber, env.SourceUUID} {
		if strings.EqualFold(strings.TrimSpace(got), want) {
			return true
		}
	}
	return false
}

func (d *Daemon) signalSource(env signalapi.Envelope) string {
	for _, v := range []string{env.SourceNumber, env.Source, env.SourceUUID, env.SourceName} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return "unknown"
}

func (d *Daemon) signalReactionDecision(raw json.RawMessage) (approval.Verdict, string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", "", false
	}
	var r struct {
		Emoji               string `json:"emoji"`
		TargetSentTimestamp int64  `json:"targetSentTimestamp"`
		TargetTimestamp     int64  `json:"targetTimestamp"`
		Timestamp           int64  `json:"timestamp"`
		Remove              bool   `json:"remove"`
		IsRemove            bool   `json:"isRemove"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || r.Remove || r.IsRemove {
		return "", "", false
	}
	verdict := signalEmojiVerdict(r.Emoji)
	if verdict == "" {
		return "", "", false
	}
	ts := r.TargetSentTimestamp
	if ts == 0 {
		ts = r.TargetTimestamp
	}
	if ts == 0 {
		ts = r.Timestamp
	}
	d.signalMu.Lock()
	id := d.signalApprovals[ts]
	d.signalMu.Unlock()
	return verdict, id, id != ""
}

func signalEmojiVerdict(emoji string) approval.Verdict {
	switch strings.TrimSpace(emoji) {
	case "\U0001f44d", "\u2705":
		return approval.VerdictApprove
	case "\U0001f44e", "\u274c":
		return approval.VerdictDeny
	default:
		return ""
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
		msg := ntfy.Message{Title: "Onibi approval", Body: formatApprovalWithPolicy(a, d.providerOutputPolicy("notify")), Tags: "warning"}
		actions, err := d.ntfyApprovalActions(a)
		if err != nil {
			d.audit(ctx, "notify.ntfy.action_error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
		} else {
			msg.Actions = actions
		}
		if err := d.publishNtfyWithRetry(ctx, c, a, msg); err != nil {
			d.audit(ctx, "notify.ntfy.error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
			return
		}
		d.audit(ctx, "notify.ntfy.sent", a.SessionID, "", 0, fmt.Sprintf("approval=%s actions=%d", a.ID, len(msg.Actions)))
	})
}

func (d *Daemon) ntfyApprovalActions(a *approval.Approval) ([]ntfy.Action, error) {
	baseURL := strings.TrimSpace(d.Ntfy.ActionBaseURL)
	if baseURL == "" {
		return nil, nil
	}
	approveURL, denyURL, err := d.signedApprovalActionURLs(baseURL, a)
	if err != nil {
		return nil, err
	}
	return []ntfy.Action{
		{Type: "http", Label: "Approve", URL: approveURL, Method: http.MethodPost, Clear: true},
		{Type: "http", Label: "Deny", URL: denyURL, Method: http.MethodPost, Clear: true},
	}, nil
}

func (d *Daemon) publishNtfyWithRetry(ctx context.Context, c *ntfy.Client, a *approval.Approval, msg ntfy.Message) error {
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := c.Publish(ctx, msg); err == nil {
			return nil
		} else {
			last = err
		}
		if attempt == 3 {
			break
		}
		d.audit(ctx, "notify.ntfy.retry", a.SessionID, "", 0, fmt.Sprintf("approval=%s attempt=%d err=%s", a.ID, attempt, last.Error()))
		timer := time.NewTimer(time.Duration(attempt) * 250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return last
}

func (d *Daemon) runGotifyNotifier(ctx context.Context, c *gotify.Client) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		body := formatApprovalWithPolicy(a, d.providerOutputPolicy("notify"))
		msg := gotify.Message{Title: "Onibi approval", Message: body, Priority: 8}
		pageURL, err := d.gotifyApprovalPageURL(a)
		if err != nil {
			d.audit(ctx, "notify.gotify.action_error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
		} else if pageURL != "" {
			msg.Message += "\n\nOpen approval: " + pageURL
			msg.Extras = gotify.ApprovalExtras(pageURL)
		}
		if err := d.publishGotifyWithRetry(ctx, c, a, msg); err != nil {
			d.audit(ctx, "notify.gotify.error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
			return
		}
		d.audit(ctx, "notify.gotify.sent", a.SessionID, "", 0, fmt.Sprintf("approval=%s action_url=%t", a.ID, pageURL != ""))
	})
}

func (d *Daemon) gotifyApprovalPageURL(a *approval.Approval) (string, error) {
	baseURL := strings.TrimSpace(d.Gotify.ActionBaseURL)
	if baseURL == "" {
		return "", nil
	}
	if d.notifyActionSigner == nil {
		return "", errors.New("notify action signer unavailable")
	}
	return d.notifyActionSigner.SignedGotifyApprovalPageURL(baseURL, a.ID, time.Now())
}

func (d *Daemon) publishGotifyWithRetry(ctx context.Context, c *gotify.Client, a *approval.Approval, msg gotify.Message) error {
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := c.Send(ctx, msg); err == nil {
			return nil
		} else {
			last = err
		}
		if attempt == 3 {
			break
		}
		d.audit(ctx, "notify.gotify.retry", a.SessionID, "", 0, fmt.Sprintf("approval=%s attempt=%d err=%s", a.ID, attempt, last.Error()))
		timer := time.NewTimer(time.Duration(attempt) * 250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return last
}

func (d *Daemon) runAPNsNotifier(ctx context.Context, c *apns.Client) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		msg := apns.PushRequest{
			DeviceToken: d.APNs.DeviceToken,
			Title:       "Onibi approval",
			Body:        formatApprovalWithPolicy(a, d.providerOutputPolicy("notify")),
			ApprovalID:  a.ID,
			CollapseID:  "onibi-" + a.ID,
			TTL:         30 * time.Second,
		}
		result, err := d.publishAPNsWithRetry(ctx, c, a, msg)
		if err != nil {
			d.audit(ctx, "notify.apns.error", a.SessionID, "", 0, fmt.Sprintf("approval=%s status=%d reason=%s err=%s", a.ID, result.StatusCode, result.Reason, err.Error()))
			return
		}
		d.audit(ctx, "notify.apns.sent", a.SessionID, "", 0, fmt.Sprintf("approval=%s status=%d apns_id=%t", a.ID, result.StatusCode, result.APNsID != ""))
	})
}

func (d *Daemon) publishAPNsWithRetry(ctx context.Context, c *apns.Client, a *approval.Approval, msg apns.PushRequest) (apns.PushResult, error) {
	var last apns.PushResult
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		result, err := c.PushApproval(ctx, msg)
		if err == nil {
			return result, nil
		}
		last = result
		lastErr = err
		if attempt == 3 {
			break
		}
		d.audit(ctx, "notify.apns.retry", a.SessionID, "", 0, fmt.Sprintf("approval=%s attempt=%d status=%d reason=%s err=%s", a.ID, attempt, result.StatusCode, result.Reason, err.Error()))
		timer := time.NewTimer(time.Duration(attempt) * 250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, ctx.Err()
		case <-timer.C:
		}
	}
	return last, lastErr
}

func (d *Daemon) runSMSNotifier(ctx context.Context, c *sms.Client) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		approveURL, denyURL, err := d.signedApprovalActionURLs(d.SMS.ActionBaseURL, a)
		if err != nil {
			d.audit(ctx, "notify.sms.action_error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
			return
		}
		msg := sms.Message{To: d.SMS.To, Body: smsApprovalBody(a, approveURL, denyURL)}
		resp, err := d.sendSMSWithRetry(ctx, c, a, msg)
		if err != nil {
			d.audit(ctx, "notify.sms.error", a.SessionID, "", 0, fmt.Sprintf("approval=%s sid=%t status=%s err=%s", a.ID, resp.SID != "", resp.Status, err.Error()))
			return
		}
		d.audit(ctx, "notify.sms.sent", a.SessionID, "", 0, fmt.Sprintf("approval=%s sid=%t status=%s", a.ID, resp.SID != "", resp.Status))
	})
}

func (d *Daemon) sendSMSWithRetry(ctx context.Context, c *sms.Client, a *approval.Approval, msg sms.Message) (sms.MessageResponse, error) {
	var last sms.MessageResponse
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		resp, err := c.Send(ctx, msg)
		if err == nil {
			return resp, nil
		}
		last = resp
		lastErr = err
		if attempt == 3 {
			break
		}
		d.audit(ctx, "notify.sms.retry", a.SessionID, "", 0, fmt.Sprintf("approval=%s attempt=%d err=%s", a.ID, attempt, err.Error()))
		timer := time.NewTimer(time.Duration(attempt) * 250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, ctx.Err()
		case <-timer.C:
		}
	}
	return last, lastErr
}

func smsApprovalBody(a *approval.Approval, approveURL, denyURL string) string {
	return fmt.Sprintf("Onibi approval %s\n%s %s\nApprove: %s\nDeny: %s", a.ID, strings.TrimSpace(a.Agent), strings.TrimSpace(a.Tool), approveURL, denyURL)
}

func (d *Daemon) runEmailNotifier(ctx context.Context, c *emailapi.Client) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		approveURL, denyURL, err := d.signedApprovalActionURLs(d.Email.ActionBaseURL, a)
		if err != nil {
			d.audit(ctx, "notify.email.action_error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
			return
		}
		msg := emailapi.Message{
			To:      d.Email.To,
			Subject: "Onibi approval " + a.ID,
			Body:    emailApprovalBody(a, d.providerOutputPolicy("notify"), approveURL, denyURL),
		}
		if err := d.sendEmailWithRetry(ctx, c, a, msg); err != nil {
			d.audit(ctx, "notify.email.error", a.SessionID, "", 0, "approval="+a.ID+" err="+err.Error())
			return
		}
		d.audit(ctx, "notify.email.sent", a.SessionID, "", 0, "approval="+a.ID)
	})
}

func (d *Daemon) sendEmailWithRetry(ctx context.Context, c *emailapi.Client, a *approval.Approval, msg emailapi.Message) error {
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := c.Send(ctx, msg); err == nil {
			return nil
		} else {
			last = err
		}
		if attempt == 3 {
			break
		}
		d.audit(ctx, "notify.email.retry", a.SessionID, "", 0, fmt.Sprintf("approval=%s attempt=%d err=%s", a.ID, attempt, last.Error()))
		timer := time.NewTimer(time.Duration(attempt) * 250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return last
}

func emailApprovalBody(a *approval.Approval, policy ProviderOutputPolicy, approveURL, denyURL string) string {
	return formatApprovalWithPolicy(a, policy) + "\n\nApprove: " + approveURL + "\nDeny: " + denyURL + "\n\nLinks expire after 5 minutes and are single-use."
}

func (d *Daemon) signedApprovalActionURLs(baseURL string, a *approval.Approval) (string, string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", "", errors.New("action base URL required")
	}
	if d.notifyActionSigner == nil {
		return "", "", errors.New("notify action signer unavailable")
	}
	approveURL, err := d.notifyActionSigner.SignedApprovalURL(baseURL, a.ID, approval.VerdictApprove, time.Now())
	if err != nil {
		return "", "", err
	}
	denyURL, err := d.notifyActionSigner.SignedApprovalURL(baseURL, a.ID, approval.VerdictDeny, time.Now())
	if err != nil {
		return "", "", err
	}
	return approveURL, denyURL, nil
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
	_, err := d.Queue.DecideIdempotently(ctx, id, verdict, "", fmt.Sprintf("provider %s", verdict), actor)
	return err
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
