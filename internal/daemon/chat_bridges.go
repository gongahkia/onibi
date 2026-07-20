//go:build !onibi_remote

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/chatout"
	"github.com/gongahkia/onibi/internal/matrix"
	"github.com/gongahkia/onibi/internal/web"
)

const (
	matrixKVSince          = "matrix.since"
	matrixKVApprovalPrefix = "matrix.approval.event."
	matrixDefaultDeviceID  = "ONIBI"
	matrixOneTimeKeyCount  = 10
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
