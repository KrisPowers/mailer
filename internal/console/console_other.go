//go:build !windows

// Package console keeps the command-line tools Mailer runs from opening
// console windows, which only happens on Windows.
package console

import "os/exec"

// Hide returns cmd unchanged: only Windows opens windows for commands.
func Hide(cmd *exec.Cmd) *exec.Cmd { return cmd }
