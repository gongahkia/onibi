package ssh

import (
	"fmt"
	"strings"
)

const (
	remoteSystemdUnit = "onibi.service"
	remoteLaunchdID   = "io.onibi"
)

type ServiceOptions struct {
	Transport string
}

func (c *Client) InstallService(platform Platform, opts ServiceOptions) error {
	if err := ValidatePlatform(platform); err != nil {
		return err
	}
	transport, err := serviceTransport(opts)
	if err != nil {
		return err
	}
	switch platform.GOOS {
	case "linux":
		return c.runRemote(systemdInstallCommand(transport))
	case "darwin":
		return c.runRemote(launchdInstallCommand(transport))
	default:
		return fmt.Errorf("ssh: unsupported service os: %s", platform.GOOS)
	}
}

func serviceTransport(opts ServiceOptions) (string, error) {
	transport := strings.TrimSpace(opts.Transport)
	if transport == "" {
		return "lan-loopback", nil
	}
	for _, r := range transport {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", fmt.Errorf("ssh: invalid service transport: %q", transport)
		}
	}
	return transport, nil
}

func systemdInstallCommand(transport string) string {
	unit := `[Unit]
Description=Onibi SSH remote daemon
After=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/onibi up --transport=` + transport + ` --no-qr
Restart=on-failure
RestartSec=5
Environment=ONIBI_SERVICE=1

[Install]
WantedBy=default.target
`
	path := `"$HOME/.config/systemd/user/` + remoteSystemdUnit + `"`
	return strings.Join([]string{
		"set -eu",
		`mkdir -p "$HOME/.config/systemd/user"`,
		writeRemoteFileCommand(path, unit),
		"systemctl --user daemon-reload",
		"systemctl --user enable --now " + remoteSystemdUnit,
	}, "\n")
}

func launchdInstallCommand(transport string) string {
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "https://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + remoteLaunchdID + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/sh</string>
    <string>-lc</string>
    <string>exec "$HOME/.local/bin/onibi" up --transport=` + transport + ` --no-qr</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>Crashed</key>
    <true/>
  </dict>
  <key>ProcessType</key>
  <string>Interactive</string>
</dict>
</plist>
`
	path := `"$HOME/Library/LaunchAgents/` + remoteLaunchdID + `.plist"`
	return strings.Join([]string{
		"set -eu",
		`mkdir -p "$HOME/Library/LaunchAgents"`,
		writeRemoteFileCommand(path, plist),
		`launchctl bootout "gui/$(id -u)" ` + path + ` >/dev/null 2>&1 || true`,
		`launchctl bootstrap "gui/$(id -u)" ` + path,
		`launchctl kickstart -k "gui/$(id -u)/` + remoteLaunchdID + `"`,
	}, "\n")
}

func writeRemoteFileCommand(pathExpr, content string) string {
	return "cat > " + pathExpr + " <<'ONIBI_REMOTE_FILE'\n" + content + "ONIBI_REMOTE_FILE"
}
