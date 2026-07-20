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
	"github.com/gongahkia/onibi/internal/chatout"
	"github.com/gongahkia/onibi/internal/matrix"
	"github.com/gongahkia/onibi/internal/slack"
	"github.com/gongahkia/onibi/internal/web"
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
