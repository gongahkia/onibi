//go:build darwin || linux

package pty

import (
	"os"
	"os/exec"
	"syscall"

	cpty "github.com/creack/pty"
)

func startPTY(cmd *exec.Cmd, rows, cols uint16, onStarted func(*os.File)) (*os.File, error) {
	master, tty, err := cpty.Open()
	if err != nil {
		return nil, err
	}
	started := false
	defer func() {
		if !started {
			_ = master.Close()
		}
	}()
	defer func() { _ = tty.Close() }()

	if err := cpty.Setsize(master, &cpty.Winsize{Rows: rows, Cols: cols}); err != nil {
		return nil, err
	}
	if cmd.Stdout == nil {
		cmd.Stdout = tty
	}
	if cmd.Stderr == nil {
		cmd.Stderr = tty
	}
	if cmd.Stdin == nil {
		cmd.Stdin = tty
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	started = true
	if onStarted != nil {
		onStarted(master)
	}
	return master, nil
}
