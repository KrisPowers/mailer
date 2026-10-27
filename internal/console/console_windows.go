// Package console keeps the command-line tools Mailer runs (tailscale,
// cloudflared, powershell) from opening console windows: the app has no
// console of its own, so Windows would give each one a new window.
package console

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// Hide makes cmd run without a window.
func Hide(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}
