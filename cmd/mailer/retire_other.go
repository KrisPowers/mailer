//go:build !windows

package main

// retireScriptInstalls only has work to do on Windows, where the old
// start.ps1 scripts ran.
func retireScriptInstalls(string) {}
