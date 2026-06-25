package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func (m *Manager) installSystemd(ctx context.Context) error {
	path, err := m.ServicePath()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, []byte(m.systemdUnit()), 0o644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}
	if out, err := m.Runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := m.Runner.Run(ctx, "systemctl", "--user", "enable", "--now", UnitName); err != nil {
		return fmt.Errorf("systemctl enable --now: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *Manager) uninstallSystemd(ctx context.Context) error {
	path, err := m.ServicePath()
	if err != nil {
		return err
	}
	_, _ = m.Runner.Run(ctx, "systemctl", "--user", "disable", "--now", UnitName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, _ = m.Runner.Run(ctx, "systemctl", "--user", "daemon-reload")
	return nil
}

func (m *Manager) systemdStatus(ctx context.Context) Status {
	path, err := m.ServicePath()
	if err != nil {
		return Status{Detail: err.Error()}
	}
	st := Status{Path: path}
	if _, err := os.Stat(path); err == nil {
		st.Installed = true
	} else if !os.IsNotExist(err) {
		st.Detail = err.Error()
	}
	out, err := m.Runner.Run(ctx, "systemctl", "--user", "is-active", UnitName)
	st.Detail = strings.TrimSpace(string(out))
	st.Running = err == nil && st.Detail == "active"
	return st
}

func (m *Manager) systemdUnit() string {
	return `[Unit]
Description=Onibi web-controlled coding-agent host
After=network-online.target

[Service]
Type=simple
ExecStart=` + systemdQuote(m.Executable) + ` run
WorkingDirectory=` + systemdQuote(m.Paths.StateDir) + `
Restart=on-abnormal
RestartSec=5
Environment=ONIBI_SERVICE=1

[Install]
WantedBy=default.target
`
}

func systemdQuote(s string) string {
	return strconv.Quote(s)
}
