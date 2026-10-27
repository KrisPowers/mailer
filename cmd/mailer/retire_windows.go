package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

// retireScriptInstalls stops copies of the server started by the old
// start.ps1 scripts, which hold the mail ports this app needs:
//   - a SYSTEM boot task running from %ProgramData%\mailer, removed with one
//     administrator prompt; its data replaces dataDir's when dataDir isn't
//     connected to Cloudflare yet
//   - a copy, and its tunnel connector, running from %LOCALAPPDATA%\mailer;
//     its exe is deleted so this only happens once
func retireScriptInstalls(dataDir string) {
	if old := filepath.Join(os.Getenv("ProgramData"), "mailer"); dirExists(old) {
		migrate := "$false"
		if !connected(dataDir) {
			migrate = "$true"
		}
		powershell(true, fmt.Sprintf(`schtasks /End /TN Mailer 2>&1 | Out-Null
schtasks /Delete /TN Mailer /F 2>&1 | Out-Null
Get-Process mailer, cloudflared -ErrorAction SilentlyContinue | Where-Object { $_.Path -like '%[1]s\*' } | Stop-Process -Force
Start-Sleep -Seconds 2
if (%[3]s -and (Test-Path '%[1]s\data')) { robocopy '%[1]s\data' '%[2]s' /E /NFL /NDL /NJH /NJS /NP | Out-Null }
Remove-Item -Recurse -Force '%[1]s'`, old, dataDir, migrate))
	}
	// Its connector shares the folder with this app's data, so it only goes
	// when that copy was running.
	local := filepath.Join(os.Getenv("LOCALAPPDATA"), "mailer")
	if _, err := os.Stat(filepath.Join(local, "mailer.exe")); err == nil {
		powershell(false, fmt.Sprintf(`$old = Get-Process mailer -ErrorAction SilentlyContinue | Where-Object { $_.Path -like '%[1]s\*' }
if ($old) {
    $old | Stop-Process -Force
    Get-Process cloudflared -ErrorAction SilentlyContinue | Where-Object { $_.Path -like '%[1]s\*' } | Stop-Process -Force
    Start-Sleep -Seconds 1
}
Remove-Item -Force '%[1]s\mailer.exe' -ErrorAction SilentlyContinue`, local))
	}
}

// retireOlderBuilds stops Mailer copies started before this exe was built,
// with their tunnel connector, so opening a new build runs it instead of
// showing the hidden window of the old one. A copy of this same build keeps
// running and its window is shown.
func retireOlderBuilds(dataDir string) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	powershell(false, fmt.Sprintf(`$built = (Get-Item -LiteralPath '%s').LastWriteTime
$old = Get-Process mailer -ErrorAction SilentlyContinue | Where-Object { $_.Id -ne %d -and $_.StartTime -and $_.StartTime -lt $built }
if ($old) {
    $old | Stop-Process -Force
    Get-Process cloudflared -ErrorAction SilentlyContinue | Where-Object { $_.Path -like '%s\*' } | Stop-Process -Force
    Start-Sleep -Seconds 1
}`, exe, os.Getpid(), dataDir))
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// connected reports whether dataDir's settings have a Cloudflare domain.
func connected(dataDir string) bool {
	b, err := os.ReadFile(filepath.Join(dataDir, "settings.json"))
	if err != nil {
		return false
	}
	var s struct {
		Cloudflare struct {
			ZoneID string         `json:"zone_id"`
			Zones  map[string]any `json:"zones"`
		} `json:"cloudflare"`
	}
	return json.Unmarshal(b, &s) == nil && (s.Cloudflare.ZoneID != "" || len(s.Cloudflare.Zones) > 0)
}

// powershell runs script with no console window, as administrator (one UAC
// prompt) when elevated is set, and waits for it.
func powershell(elevated bool, script string) {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	enc := base64.StdEncoding.EncodeToString(b)
	args := []string{"-NoProfile", "-NonInteractive", "-EncodedCommand", enc}
	if elevated {
		args = []string{"-NoProfile", "-NonInteractive", "-Command",
			"Start-Process powershell -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList '" +
				strings.Join([]string{"-NoProfile", "-NonInteractive", "-EncodedCommand", enc}, " ") + "'"}
	}
	cmd := exec.Command("powershell", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	cmd.Run()
}
