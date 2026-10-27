package wslclient

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMapPath(t *testing.T) {
	base := `C:\work\app`
	tests := []struct {
		path, linux, drive string
		err                bool
	}{
		{`C:\a b\c`, "", `C:\a b\c`, false},
		{`c:/a b/./c/../d`, "", `C:\a b\d`, false},
		{`src\pkg`, "", `C:\work\app\src\pkg`, false},
		{`..\other`, "", `C:\work\other`, false},
		{`\root`, "", `C:\root`, false},
		{`\\wsl.localhost\Ubuntu\home\u`, "/home/u", "", false},
		{`\\wsl$\ubuntu\tmp`, "/tmp", "", false},
		{`\\WSL.LOCALHOST\Ubuntu`, "/", "", false},
		{`\\wsl.localhost\Debian\home\u`, "", "", true},
		{`\\server\share\proj`, "", "", true},
		{`\\?\C:\x`, "", "", true},
		{`C:relative`, "", "", true},
		{"/", "/", "", false},
		{"~", "~", "", false},
		{"~/proj", "~/proj", "", false},
	}
	for _, tt := range tests {
		linux, drive, err := MapPath(tt.path, base, "Ubuntu")
		if linux != tt.linux || drive != tt.drive || (err != nil) != tt.err {
			t.Errorf("%s: linux %q drive %q err %v", tt.path, linux, drive, err)
		}
		if tt.err && !errors.Is(err, ErrUnmappable) {
			t.Errorf("%s: error %v", tt.path, err)
		}
	}
	// A relative path inside the distribution's own share stays mapped.
	if linux, _, err := MapPath("sub", `\\wsl.localhost\Ubuntu\home\u`, "Ubuntu"); linux != "/home/u/sub" || err != nil {
		t.Fatalf("relative in share: %q %v", linux, err)
	}
}

func TestResolveTarget(t *testing.T) {
	dir := t.TempDir()
	settings := TraySettingsPath(dir)
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	if got := ResolveTarget("", "", getenv, settings); got != (Target{Webmux: DefaultWebmux}) {
		t.Fatalf("defaults %+v", got)
	}
	if err := os.MkdirAll(filepath.Dir(settings), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte("\xef\xbb\xbf{\"distro\":\"Debian\",\"webmux\":\"/opt/webmux\",\"mode\":\"auto\"}"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveTarget("", "", getenv, settings); got != (Target{Distro: "Debian", Webmux: "/opt/webmux"}) {
		t.Fatalf("tray settings %+v", got)
	}
	env["WEBMUX_DISTRO"], env["WEBMUX_WSL_PATH"] = "Arch", "~/bin/webmux"
	if got := ResolveTarget("", "", getenv, settings); got != (Target{Distro: "Arch", Webmux: "~/bin/webmux"}) {
		t.Fatalf("environment %+v", got)
	}
	if got := ResolveTarget("Ubuntu", "/usr/bin/webmux", getenv, settings); got != (Target{Distro: "Ubuntu", Webmux: "/usr/bin/webmux"}) {
		t.Fatalf("flags %+v", got)
	}
	env = map[string]string{}
	if err := os.WriteFile(settings, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveTarget("", "", getenv, settings); got != (Target{Webmux: DefaultWebmux}) {
		t.Fatalf("corrupt settings %+v", got)
	}
	target := Target{Distro: "Ubuntu", Webmux: "~/.local/bin/webmux"}
	if got := target.Bridge(); !slices.Equal(got, []string{"wsl.exe", "-d", "Ubuntu", "--cd", "~", "--exec", "./.local/bin/webmux", "bridge"}) {
		t.Fatalf("bridge argv %q", got)
	}
	if got := target.Wslpath(`C:\a b;$(id)`); !slices.Equal(got, []string{"wsl.exe", "-d", "Ubuntu", "--exec", "wslpath", "-a", "-u", `C:\a b;$(id)`}) {
		t.Fatalf("wslpath argv %q", got)
	}
}
