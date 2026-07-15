package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/store"
)

const maxFleetBudgetTokens = int64(^uint64(0) >> 1)

func (s *Server) evaluateFleetBudget(ctx context.Context, day string) error {
	if s == nil || s.db == nil {
		return nil
	}
	hosts, err := s.db.FleetHostList(ctx)
	if err != nil {
		return err
	}
	var total, limit int64
	action := "warn"
	type target struct {
		hostID    string
		sessionID string
	}
	targets := make([]target, 0)
	for _, host := range hosts {
		if host.State != fleet.HostStateActive || host.Budget.Date != day {
			continue
		}
		if host.Budget.DailyTokens > maxFleetBudgetTokens-total {
			total = maxFleetBudgetTokens
		} else {
			total += host.Budget.DailyTokens
		}
		if host.Budget.GlobalLimit > 0 {
			if limit == 0 || host.Budget.GlobalLimit < limit {
				limit = host.Budget.GlobalLimit
				action = host.Budget.OnOverrun
			} else if host.Budget.GlobalLimit == limit {
				action = stricterFleetBudgetAction(action, host.Budget.OnOverrun)
			}
		}
		for _, session := range host.Budget.Sessions {
			targets = append(targets, target{hostID: host.ID, sessionID: session.SessionID})
		}
	}
	if limit == 0 || total <= limit || action == "warn" {
		return nil
	}
	for _, target := range targets {
		if err := s.dispatchFleetBudgetControl(ctx, day, target.hostID, target.sessionID, action, total, limit); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) dispatchFleetBudgetControl(ctx context.Context, day, hostID, sessionID, action string, total, limit int64) error {
	payload, err := json.Marshal(fleet.ControlPayload{SessionID: sessionID})
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	command, created, err := s.db.ControlCommandCreate(ctx, store.ControlCommand{
		ID:        fleetBudgetCommandID(day, hostID, sessionID, action, limit),
		HostID:    hostID,
		SessionID: sessionID,
		Action:    action,
		Payload:   payload,
		State:     fleet.CommandPending,
		CreatedAt: now,
		ExpiresAt: now.Add(controlCommandTTL),
	})
	if err != nil {
		return err
	}
	if created {
		if err := s.db.AuditAppend(ctx, "budget.global.overrun", sessionID, "", 0, fmt.Sprintf("host=%s action=%s tokens=%d limit=%d", hostID, action, total, limit)); err != nil {
			return err
		}
	}
	if command.State.Terminal() {
		return nil
	}
	if err := s.dispatchFleetControl(ctx, command); err != nil {
		s.log.Warn("dispatch fleet budget control", "command_id", command.ID, "host_id", hostID, "err", err)
	}
	return nil
}

func fleetBudgetCommandID(day, hostID, sessionID, action string, limit int64) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{day, hostID, sessionID, action, fmt.Sprintf("%d", limit)}, "\n")))
	return "budget-" + hex.EncodeToString(sum[:24])
}

func isFleetBudgetCommand(id string) bool {
	return strings.HasPrefix(id, "budget-")
}

func stricterFleetBudgetAction(current, next string) string {
	if fleetBudgetActionRank(next) > fleetBudgetActionRank(current) {
		return next
	}
	return current
}

func fleetBudgetActionRank(action string) int {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "kill":
		return 3
	case "interrupt":
		return 2
	default:
		return 1
	}
}
