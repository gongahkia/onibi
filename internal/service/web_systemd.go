package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func (m WebManager) webSystemctl(ctx context.Context, args ...string) error {
	out, err := m.Runner.Run(ctx, "systemctl", append([]string{"--user"}, args...)...)
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %w: %s; a running systemd user manager is required (on WSL, enable systemd and restart the distro), or run onibi web in the foreground", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m WebManager) installWebSystemd(ctx context.Context, path, listen, origin string) error {
	// Check the user manager before leaving a unit on a non-systemd host.
	if err := m.webSystemctl(ctx, "show", "--property=Version", "--value"); err != nil {
		return err
	}
	if err := writeFileAtomic(path, []byte(m.webSystemdUnit(listen, origin)), 0600); err != nil {
		return err
	}
	if err := m.webSystemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	// Match launchd: install enables future logins; start is explicit.
	return m.webSystemctl(ctx, "enable", WebUnitName)
}

// systemdLiteral quotes one unit value and prevents specifier expansion.
// ExecStart also uses the ':' prefix to disable $ environment substitution.
func systemdLiteral(value string) string {
	return strconv.Quote(strings.ReplaceAll(value, "%", "%%"))
}

func (m WebManager) webSystemdUnit(listen, origin string) string {
	var env strings.Builder
	for _, key := range []string{"PATH", "SHELL", "XDG_DATA_HOME", "XDG_RUNTIME_DIR", "XDG_CONFIG_HOME"} {
		fmt.Fprintf(&env, "Environment=%s\n", systemdLiteral(key+"="+os.Getenv(key)))
	}
	return `[Unit]
Description=Onibi web cockpit and persistent sessions
After=network.target

[Service]
Type=simple
ExecStart=:` + systemdLiteral(m.Executable) + ` web --background --listen ` + systemdLiteral(listen) + ` --origin ` + systemdLiteral(origin) + `
Restart=on-failure
RestartSec=5
TimeoutStopSec=60
# Multiplexer sessions must survive a web-server stop or restart.
KillMode=process
UMask=0077
Environment=TERM=xterm-256color
` + env.String() + `
[Install]
WantedBy=default.target
`
}
