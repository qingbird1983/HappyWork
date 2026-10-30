//go:build !windows

package mcp

import "os/exec"

func hideWindow(_ *exec.Cmd) {}
