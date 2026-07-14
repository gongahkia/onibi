package fleet

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type HomeStatus struct {
	Version          uint16               `json:"version"`
	GeneratedAt      time.Time            `json:"generated_at"`
	Hosts            []Host               `json:"hosts"`
	Sessions         []HomeSessionStatus  `json:"sessions"`
	PendingApprovals []HomeApprovalStatus `json:"pending_approvals"`
}

type HomeSessionStatus struct {
	ID                string               `json:"id"`
	HostID            string               `json:"host_id,omitempty"`
	Agent             string               `json:"agent"`
	State             string               `json:"state"`
	LastActivity      time.Time            `json:"last_activity"`
	PendingApprovals  int                  `json:"pending_approvals"`
	RecoveryState     SessionRecoveryState `json:"recovery_state,omitempty"`
	RecoveryReason    string               `json:"recovery_reason,omitempty"`
	RecoveryUpdatedAt time.Time            `json:"recovery_updated_at,omitempty"`
	Remote            bool                 `json:"remote,omitempty"`
	PeerName          string               `json:"peer_name,omitempty"`
}

type HomeApprovalStatus struct {
	ID        string    `json:"id"`
	HostID    string    `json:"host_id,omitempty"`
	SessionID string    `json:"session_id"`
	Agent     string    `json:"agent"`
	Tool      string    `json:"tool"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s HomeStatus) Validate() error {
	if s.Version != ProtocolVersion {
		return fmt.Errorf("fleet home status version %d is incompatible with %d", s.Version, ProtocolVersion)
	}
	if s.GeneratedAt.IsZero() {
		return errors.New("fleet home status generated_at required")
	}
	hosts := make(map[string]bool, len(s.Hosts))
	for _, host := range s.Hosts {
		host = host.Normalized()
		if err := host.Validate(); err != nil {
			return err
		}
		if hosts[host.ID] {
			return fmt.Errorf("duplicate fleet host %q", host.ID)
		}
		hosts[host.ID] = true
	}
	sessions := make(map[string]bool, len(s.Sessions))
	sessionHosts := make(map[string]string, len(s.Sessions))
	for _, session := range s.Sessions {
		if !validID(session.ID) || strings.TrimSpace(session.Agent) == "" || !validHomeSessionState(session.State) || session.LastActivity.IsZero() || session.PendingApprovals < 0 {
			return errors.New("invalid fleet home session")
		}
		if session.RecoveryState == "" {
			if strings.TrimSpace(session.RecoveryReason) != "" || !session.RecoveryUpdatedAt.IsZero() {
				return errors.New("invalid fleet home session recovery")
			}
		} else if !session.RecoveryState.Valid() || session.RecoveryUpdatedAt.IsZero() || len(session.RecoveryReason) > 512 || (session.RecoveryState == SessionRecoveryHealthy && strings.TrimSpace(session.RecoveryReason) != "") || (session.RecoveryState != SessionRecoveryHealthy && strings.TrimSpace(session.RecoveryReason) == "") {
			return errors.New("invalid fleet home session recovery")
		}
		if sessions[session.ID] {
			return fmt.Errorf("duplicate fleet home session %q", session.ID)
		}
		if session.HostID != "" && !hosts[session.HostID] {
			return fmt.Errorf("fleet home session %q references unknown host %q", session.ID, session.HostID)
		}
		if session.Remote && session.HostID == "" {
			return fmt.Errorf("remote fleet home session %q requires host", session.ID)
		}
		sessions[session.ID] = true
		sessionHosts[session.ID] = session.HostID
	}
	for _, approval := range s.PendingApprovals {
		if !validID(approval.ID) || !validID(approval.SessionID) || strings.TrimSpace(approval.Agent) == "" || strings.TrimSpace(approval.Tool) == "" || approval.State != "pending" || approval.CreatedAt.IsZero() || !approval.ExpiresAt.After(approval.CreatedAt) {
			return errors.New("invalid fleet home approval")
		}
		if approval.HostID != "" && !hosts[approval.HostID] {
			return fmt.Errorf("fleet home approval %q references unknown host %q", approval.ID, approval.HostID)
		}
		if hostID, ok := sessionHosts[approval.SessionID]; ok && approval.HostID != hostID {
			return fmt.Errorf("fleet home approval %q host does not match session", approval.ID)
		}
	}
	return nil
}

func validHomeSessionState(state string) bool {
	switch state {
	case "idle", "working", "awaiting-approval", "blocked":
		return true
	default:
		return false
	}
}
