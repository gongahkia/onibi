package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gongahkia/onibi/internal/web"
)

const WebLabel = "sh.onibi.web"

// WebManager manages the native web cockpit in the user's graphical login
// session, independently of the Telegram daemon. No system/root service exists.
type WebManager struct{ *Manager }

func (m WebManager) path() (string, error) {
	if err := m.ensure(); err != nil {
		return "", err
	}
	if m.GOOS != "darwin" {
		return "", errors.New("the web service requires a macOS graphical login session")
	}
	return filepath.Join(m.Home, "Library", "LaunchAgents", WebLabel+".plist"), nil
}
func (m WebManager) target() string { return fmt.Sprintf("gui/%d/%s", m.UID, WebLabel) }
func (m WebManager) Install(ctx context.Context, listen, origin string) error {
	path, err := m.path()
	if err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(listen)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || number < 1 || number > 65535 || (host != "localhost" && !net.ParseIP(host).IsLoopback()) {
		return errors.New("web service requires a fixed loopback listen address")
	}
	if origin == "" {
		origin = "http://" + listen
	}
	if err := web.ValidateOrigin(origin); err != nil {
		return err
	}
	if err := m.Paths.EnsureDirs(); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return errors.New("web service is already installed; remove it before changing its configuration")
	} else if !os.IsNotExist(err) {
		return err
	}
	vals := map[string]any{
		"Label":            WebLabel,
		"ProgramArguments": []string{m.Executable, "web", "--background", "--listen", listen, "--origin", origin},
		"RunAtLoad":        true, "KeepAlive": map[string]any{"Crashed": true},
		"ProcessType": "Interactive", "LimitLoadToSessionType": "Aqua", "ExitTimeOut": 45,
		"WorkingDirectory":     m.Paths.StateDir,
		"EnvironmentVariables": map[string]string{"PATH": "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"},
		"StandardOutPath":      filepath.Join(m.Paths.LogDir, "web.out.log"),
		"StandardErrorPath":    filepath.Join(m.Paths.LogDir, "web.err.log"),
	}
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString("<plist version=\"1.0\">\n")
	writePlistDict(&b, vals)
	b.WriteString("</plist>\n")
	// Installation is reviewable and does not start native automation. Start is
	// separate so the operator can inspect the address and graphical permissions.
	return writeFileAtomic(path, b.Bytes(), 0600)
}
func (m WebManager) Start(ctx context.Context) error {
	path, err := m.path()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("install the web service first: %w", err)
	}
	if _, err := m.Runner.Run(ctx, "launchctl", "print", m.target()); err == nil {
		out, err := m.Runner.Run(ctx, "launchctl", "kickstart", m.target())
		return launchError("start", out, err)
	}
	out, err := m.Runner.Run(ctx, "launchctl", "bootstrap", fmt.Sprintf("gui/%d", m.UID), path)
	return launchError("start in the graphical login session", out, err)
}
func (m WebManager) Stop(ctx context.Context) error {
	if _, err := m.path(); err != nil {
		return err
	}
	out, err := m.Runner.Run(ctx, "launchctl", "bootout", m.target())
	// bootout unloads the job; a crash policy cannot respawn a deliberately
	// stopped service. The installed LaunchAgent runs again at the next login.
	if err != nil && (strings.Contains(string(out), "No such process") || strings.Contains(string(out), "Could not find service")) {
		return nil
	}
	return launchError("stop", out, err)
}
func (m WebManager) Restart(ctx context.Context) error {
	if err := m.Stop(ctx); err != nil {
		return err
	}
	return m.Start(ctx)
}
func (m WebManager) Remove(ctx context.Context) error {
	path, err := m.path()
	if err != nil {
		return err
	}
	if err := m.Stop(ctx); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
func (m WebManager) Status(ctx context.Context) Status {
	path, err := m.path()
	if err != nil {
		return Status{Detail: err.Error()}
	}
	st := Status{Path: path}
	if _, err := os.Stat(path); err == nil {
		st.Installed = true
	} else if !os.IsNotExist(err) {
		st.Detail = err.Error()
		return st
	}
	out, err := m.Runner.Run(ctx, "launchctl", "print", m.target())
	if err == nil {
		_, st.Running = launchdOutputPID(out)
		st.Detail = firstLine(out)
	} else {
		st.Detail = strings.TrimSpace(string(out))
		if st.Detail == "" {
			st.Detail = err.Error()
		}
	}
	return st
}
func launchError(action string, out []byte, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("could not %s web service: %w: %s", action, err, strings.TrimSpace(string(out)))
}
