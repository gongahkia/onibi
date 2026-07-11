//go:build !darwin && !linux

package pty

import (
	"os"
	"os/exec"

	cpty "github.com/creack/pty"
)

func startPTY(cmd *exec.Cmd, rows, cols uint16, onStarted func(*os.File)) (*os.File, error) {
	master, err := cpty.StartWithSize(cmd, &cpty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return nil, err
	}
	if onStarted != nil {
		onStarted(master)
	}
	return master, nil
}
