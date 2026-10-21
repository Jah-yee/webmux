package session

import "strings"

// Identity variables of the terminal or multiplexer that started webmux. Left
// in place they make programs target that host (for example DCS-wrapping
// output for tmux) instead of the browser terminal behind this PTY.
var hostTerminalNames = map[string]bool{
	"TERM_PROGRAM": true, "TERM_PROGRAM_VERSION": true, "TMUX": true, "TMUX_PANE": true,
	"STY": true, "LC_TERMINAL": true, "LC_TERMINAL_VERSION": true, "WT_SESSION": true,
	"WT_PROFILE_ID": true, "VTE_VERSION": true,
	// Describes how the server was started, not the session's programs.
	"WEBMUX_LAUNCHER": true,
	// Set explicitly below.
	"TERM": true, "COLORTERM": true, "WEBMUX_SESSION_ID": true,
}
var hostTerminalPrefixes = []string{"WARP_", "ZELLIJ", "KITTY_", "ITERM_", "WEZTERM_", "GHOSTTY_"}

// sessionEnv advertises only the Warp cli-agent protocol version, never a Warp
// client identity, so agents emit structured events without Warp-only features.
func sessionEnv(base []string, id string, agentProtocol bool) []string {
	env := make([]string, 0, len(base)+4)
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if hostTerminalNames[name] || hasAnyPrefix(name, hostTerminalPrefixes) {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "WEBMUX_SESSION_ID="+id)
	if agentProtocol {
		env = append(env, "WARP_CLI_AGENT_PROTOCOL_VERSION=1")
	}
	return env
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
