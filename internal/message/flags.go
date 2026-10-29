package message

import (
	"mime"
	"strings"
)

// riskyExt are attachment types that run code when opened: programs,
// scripts, shortcuts, macro-enabled Office files and disk images.
var riskyExt = map[string]bool{
	"exe": true, "scr": true, "com": true, "pif": true, "bat": true, "cmd": true, "cpl": true,
	"msi": true, "msp": true, "jar": true, "js": true, "jse": true, "vbs": true, "vbe": true,
	"wsf": true, "wsh": true, "ps1": true, "hta": true, "lnk": true, "reg": true, "scf": true,
	"iso": true, "img": true, "vhd": true, "vhdx": true,
	"docm": true, "dotm": true, "xlsm": true, "xltm": true, "xlam": true, "pptm": true, "potm": true, "ppam": true,
}

// RiskyFiles returns the names of the message's risky attachments.
func RiskyFiles(raw []byte) []string {
	var out []string
	Parse(raw).Walk(func(p *Part) {
		name := p.Params["name"]
		if _, params, err := mime.ParseMediaType(p.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
			name = params["filename"]
		}
		name = strings.TrimSpace(DecodeHeader(name))
		if i := strings.LastIndexByte(name, '.'); i >= 0 && riskyExt[strings.ToLower(name[i+1:])] {
			out = append(out, name)
		}
	})
	return out
}

// Bulk reports whether the header marks a newsletter or marketing send.
func Bulk(h Header) bool {
	if h.Has("List-Unsubscribe") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(h.Get("Precedence"))) {
	case "bulk", "list", "junk":
		return true
	}
	return false
}
