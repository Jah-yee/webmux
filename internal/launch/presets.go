package launch

// Presets returns the launch configs seeded into a new installation: a login
// shell and common CLI agents. They are ordinary configs the user may edit or
// delete. shell is the login shell path; empty means /bin/sh.
func Presets(shell string) []Input {
	if shell == "" {
		shell = "/bin/sh"
	}
	return []Input{
		{Name: "Shell", Cmd: []string{shell, "-l", "-i"}},
		{Name: "Claude Code", Cmd: bashrcCommand("claude")},
		{Name: "Codex CLI", Cmd: bashrcCommand("codex")},
		{Name: "Gemini CLI", Cmd: bashrcCommand("gemini")},
		{Name: "Open Code", Cmd: bashrcCommand("opencode")},
		{Name: "omp", Cmd: bashrcCommand("omp")},
	}
}

// bashrcCommand runs program through an interactive bash so it sees the PATH
// and environment from ~/.bashrc, which a service manager never loads. It is
// the argv the settings editor saves for "加载 ~/.bashrc" (BASHRC_PREFIX in
// web/src/launchConfigs.ts), so the editor shows the plain program checked.
func bashrcCommand(program string) []string {
	return []string{"bash", "-ic", `exec -- "$@"`, "webmux", program}
}
