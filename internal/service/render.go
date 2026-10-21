package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"
)

func serviceEnvironment(home string) map[string]string {
	// Capture only the environment needed by shells/agents. Never persist the
	// whole environment, which may contain credentials and terminal identity.
	var paths []string
	for _, path := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.IsAbs(path) && !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	for _, path := range []string{
		filepath.Join(home, ".local", "bin"), filepath.Join(home, ".local", "share", "pnpm"),
		filepath.Join(home, ".npm-global", "bin"), "/opt/homebrew/bin", "/usr/local/bin",
		"/usr/bin", "/bin", "/usr/sbin", "/sbin",
	} {
		if !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	env := map[string]string{"PATH": strings.Join(paths, ":"), "LANG": "en_US.UTF-8"}
	for _, key := range []string{"SHELL", "LANG", "LC_CTYPE", "LC_ALL", "XDG_STATE_HOME"} {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}
	return env
}

// Unit quoted values use C escapes and expand % specifiers. ExecStart is
// prefixed with ':' to disable $ expansion for executable/config paths.
func unitQuote(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%", "\n", "\\n", "\r", "\\r", "\t", "\\t")
	return "\"" + replacer.Replace(value) + "\""
}

func (m *Manager) render(path string) ([]byte, error) {
	if m.platform == "linux" {
		var body strings.Builder
		fmt.Fprintf(&body, `[Unit]
Description=WebMux persistent web terminal
After=network-online.target

[Service]
Type=exec
ExecStart=:%s serve --config %s
WorkingDirectory=%s
Restart=on-failure
RestartSec=2
KillMode=mixed
TimeoutStopSec=15
UMask=0077
`, unitQuote(m.executable), unitQuote(path), strings.ReplaceAll(m.home, "%", "%%"))
		keys := make([]string, 0, len(m.environment))
		for key := range m.environment {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(&body, "Environment=%s\n", unitQuote(key+"="+m.environment[key]))
		}
		body.WriteString("\n[Install]\nWantedBy=default.target\n")
		return []byte(body.String()), nil
	}
	// text/template with explicit XML escaping handles spaces, &, quotes and
	// non-ASCII paths without invoking a shell or relying on ~ expansion.
	tpl := template.Must(template.New("launchd").Funcs(template.FuncMap{
		"xml": func(value string) (string, error) {
			var out bytes.Buffer
			err := xml.EscapeText(&out, []byte(value))
			return out.String(), err
		},
	}).Parse(launchdTemplate))
	var body bytes.Buffer
	err := tpl.Execute(&body, struct {
		Executable, Config, Home, Log string
		Environment                   map[string]string
	}{m.executable, path, m.home, m.logPath(), m.environment})
	return body.Bytes(), err
}

const launchdTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.webmux</string>
  <key>ProgramArguments</key>
  <array><string>{{xml .Executable}}</string><string>serve</string><string>--config</string><string>{{xml .Config}}</string></array>
  <key>WorkingDirectory</key><string>{{xml .Home}}</string>
  <key>EnvironmentVariables</key>
  <dict>{{range $key, $value := .Environment}}
    <key>{{xml $key}}</key><string>{{xml $value}}</string>{{end}}
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
  <key>ThrottleInterval</key><integer>2</integer>
  <key>ExitTimeOut</key><integer>15</integer>
  <key>Umask</key><integer>63</integer>
  <key>StandardOutPath</key><string>{{xml .Log}}</string>
  <key>StandardErrorPath</key><string>{{xml .Log}}</string>
</dict>
</plist>
`
