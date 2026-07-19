package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/store"
)

func (d *Daemon) SendSessionKey(ctx context.Context, id, key string) error {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return err
	}
	if s.Transport == "tmux" && s.TmuxTarget != "" {
		return newTmuxController().SendKey(ctx, s.TmuxTarget, key)
	}
	if s.Host == nil {
		return errors.New("session has no writable PTY")
	}
	switch key {
	case "Enter":
		_, err = s.Host.Write([]byte{'\n'})
	case "Escape":
		_, err = s.Host.Write([]byte{0x1b})
	default:
		err = errors.New("unsupported key")
	}
	return err
}

func (d *Daemon) ControlSession(ctx context.Context, id, action string) error {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return err
	}
	if s.Transport == "tmux" && s.TmuxTarget != "" {
		ctrl := newTmuxController()
		switch action {
		case "interrupt":
			return ctrl.SendKey(ctx, s.TmuxTarget, "C-c")
		case "kill":
			if err := ctrl.KillSession(ctx, s.TmuxTarget); err != nil {
				return err
			}
			d.markSessionEnded(ctx, s)
			return nil
		default:
			return errors.New("unsupported action")
		}
	}
	if s.Host == nil {
		return errors.New("session has no writable PTY")
	}
	switch action {
	case "interrupt":
		_, err = s.Host.Write([]byte{3})
		return err
	case "kill":
		if err := s.Host.Close(); err != nil {
			return err
		}
		d.markSessionEnded(ctx, s)
		return nil
	default:
		return errors.New("unsupported action")
	}
}

func (d *Daemon) handleFleetControl(ctx context.Context, control fleet.Control) fleet.ControlResult {
	result := fleet.ControlResult{Version: fleet.ProtocolVersion, ID: control.ID, OwnerID: control.OwnerID, HostID: control.HostID, State: fleet.CommandFailed, Error: "control execution failed", CompletedAt: time.Now().UTC()}
	if d == nil || d.DB == nil {
		result.Error = "command store unavailable"
		return result
	}
	var payload fleet.ControlPayload
	if err := json.Unmarshal(control.Payload, &payload); err != nil || strings.TrimSpace(payload.SessionID) == "" {
		result.Error = "invalid control payload"
		return result
	}
	command, created, err := d.DB.ControlCommandCreate(ctx, store.ControlCommand{ID: control.ID, HostID: control.HostID, SessionID: payload.SessionID, Action: control.Command, Payload: control.Payload, State: fleet.CommandPending, CreatedAt: time.Now().UTC(), ExpiresAt: control.ExpiresAt.UTC()})
	if err != nil {
		result.Error = fleetControlError(err)
		return result
	}
	if !created {
		if !command.State.Terminal() {
			_, _ = d.DB.ControlCommandComplete(ctx, command.ID, fleet.CommandTimedOut, "command recovery required", time.Now().UTC())
			command, _ = d.DB.ControlCommand(ctx, command.ID)
		}
		return fleetControlResult(control, command)
	}
	if !control.ExpiresAt.After(time.Now().UTC()) {
		_, _ = d.DB.ControlCommandComplete(ctx, command.ID, fleet.CommandTimedOut, "command timed out", time.Now().UTC())
		command, _ = d.DB.ControlCommand(ctx, command.ID)
		return fleetControlResult(control, command)
	}
	message, err := d.executeFleetControl(ctx, command, payload)
	state := fleet.CommandSucceeded
	if err != nil {
		state = fleet.CommandFailed
		message = fleetControlError(err)
	}
	if _, completeErr := d.DB.ControlCommandComplete(ctx, command.ID, state, message, time.Now().UTC()); completeErr != nil {
		result.Error = fleetControlError(completeErr)
		return result
	}
	command, err = d.DB.ControlCommand(ctx, command.ID)
	if err != nil {
		result.Error = fleetControlError(err)
		return result
	}
	return fleetControlResult(control, command)
}

func (d *Daemon) executeFleetControl(ctx context.Context, command store.ControlCommand, payload fleet.ControlPayload) (string, error) {
	switch command.Action {
	case "interrupt", "kill":
		return "", d.ControlSession(ctx, command.SessionID, command.Action)
	case "input":
		if strings.TrimSpace(payload.Input) == "" {
			return "", errors.New("control input required")
		}
		_, err := d.SendSessionTextAndCapture(ctx, command.SessionID, payload.Input, true)
		return "", err
	case "handover":
		if strings.TrimSpace(payload.Target) == "" {
			return "", errors.New("handover target required")
		}
		return d.HandoverSession(ctx, command.SessionID, payload.Target)
	default:
		return "", errors.New("unsupported control action")
	}
}

func fleetControlResult(control fleet.Control, command store.ControlCommand) fleet.ControlResult {
	result := fleet.ControlResult{Version: fleet.ProtocolVersion, ID: control.ID, OwnerID: control.OwnerID, HostID: control.HostID, State: command.State, CompletedAt: command.CompletedAt}
	if !result.State.Terminal() {
		result.State = fleet.CommandTimedOut
		result.Error = "command recovery required"
		result.CompletedAt = time.Now().UTC()
	} else if result.State == fleet.CommandSucceeded {
		result.Result = command.Result
	} else {
		result.Error = command.Result
	}
	return result
}
