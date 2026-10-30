//go:build windows

package mcp

import (
	"os/exec"
	"syscall"
)

// hideWindow prevents a console flash for stdio servers on Windows.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
