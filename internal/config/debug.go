package config

import (
	"encoding/json"
	"fmt"
	"net"
)

// DebugProfile explicitly chooses an adapter; project files never start one.
type DebugProfile struct {
	Command   []string       `json:"command"`
	Address   string         `json:"address"`
	Request   string         `json:"request"`
	Arguments map[string]any `json:"arguments"`
}

func (p DebugProfile) Validate() error {
	if (len(p.Command) == 0) == (p.Address == "") {
		return fmt.Errorf("choose either a command array or a TCP address")
	}
	if len(p.Command) > 64 || (len(p.Command) > 0 && p.Command[0] == "") {
		return fmt.Errorf("command needs 1–64 arguments")
	}
	if p.Address != "" {
		if _, _, err := net.SplitHostPort(p.Address); err != nil {
			return err
		}
	}
	if p.Request != "launch" && p.Request != "attach" {
		return fmt.Errorf("request must be launch or attach")
	}
	raw, err := json.Marshal(p.Arguments)
	if err != nil {
		return err
	}
	if len(raw) > 64<<10 {
		return fmt.Errorf("debug arguments exceed 64 KiB")
	}
	return nil
}
