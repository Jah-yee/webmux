package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCustomPresetNameIsReserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[[presets]]\nname = \"custom\"\nlabel = \"Configured command\"\ncmd = [\"/bin/echo\"]\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("custom preset validation = %v, want reserved-name error", err)
	}
}

func TestStateDirectoryDefaultsAndOptionalStorage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	if got := DefaultStateDir(); got != filepath.Join(home, ".local", "state", "webmux") {
		t.Fatalf("home state directory: %q", got)
	}
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	c, err := Load(filepath.Join(t.TempDir(), "absent"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.StateDir != filepath.Join(state, "webmux") {
		t.Fatalf("XDG state directory: %q", c.StateDir)
	}
	c.StateDir = "/missing/unwritable/state"
	if err := c.Validate(); err != nil {
		t.Fatalf("optional storage prevented startup: %v", err)
	}
}

func TestTunnelConfiguration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	for _, row := range []struct {
		name, body string
		err        string // substring; empty means valid
		hostname   string
		mirrors    []string
	}{
		{name: "absent", body: ""},
		{name: "quick needs no hostname", body: "enabled = true\n"},
		{name: "named", body: "enabled = true\ntoken = 'cred'\nhostname = 'Term.Example.COM'\n", hostname: "term.example.com"},
		{name: "named disabled without hostname", body: "token = 'cred'\n"},
		{name: "named missing hostname", body: "enabled = true\ntoken = 'cred'\n", err: "tunnel.hostname is required"},
		{name: "hostname trailing dot", body: "hostname = 'Term.Example.COM.'\n", hostname: "term.example.com"},
		{name: "hostname wildcard", body: "hostname = '*.example.com'\n", err: "not a wildcard"},
		{name: "hostname unicode", body: "hostname = '终端.example.com'\n", err: "ASCII/punycode"},
		{name: "hostname scheme", body: "hostname = 'https://term.example.com'\n", err: "bare hostname"},
		{name: "hostname port", body: "hostname = 'term.example.com:443'\n", err: "bare hostname"},
		{name: "hostname path", body: "hostname = 'term.example.com/x'\n", err: "bare hostname"},
		{name: "hostname credentials", body: "hostname = 'u@term.example.com'\n", err: "bare hostname"},
		{name: "empty mirrors", body: "download_mirrors = []\n", mirrors: []string{}},
		{name: "mirror slash", body: "download_mirrors = ['https://ghfast.top/', 'https://example.com/gh']\n", mirrors: []string{"https://ghfast.top", "https://example.com/gh"}},
		{name: "mirror http", body: "download_mirrors = ['http://ghfast.top']\n", err: "download_mirrors"},
		{name: "mirror query", body: "download_mirrors = ['https://ghfast.top/?x=1']\n", err: "download_mirrors"},
		{name: "mirror fragment", body: "download_mirrors = ['https://ghfast.top#x']\n", err: "download_mirrors"},
		{name: "mirror credentials", body: "download_mirrors = ['https://u:p@ghfast.top']\n", err: "download_mirrors"},
	} {
		t.Run(row.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(row.name, " ", "-")+".toml")
			if err := os.WriteFile(path, []byte("[tunnel]\n"+row.body), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := Load(path, true)
			if err != nil {
				t.Fatal(err)
			}
			err = c.Validate()
			if row.err != "" {
				if err == nil || !strings.Contains(err.Error(), row.err) {
					t.Fatalf("Validate() = %v, want %q", err, row.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Tunnel.Hostname != row.hostname {
				t.Fatalf("hostname %q, want %q", c.Tunnel.Hostname, row.hostname)
			}
			if (c.Tunnel.DownloadMirrors == nil) != (row.mirrors == nil) || !reflect.DeepEqual(c.Tunnel.DownloadMirrors, row.mirrors) && row.mirrors != nil {
				t.Fatalf("mirrors %#v, want %#v", c.Tunnel.DownloadMirrors, row.mirrors)
			}
		})
	}
}

func TestTunnelCloudflaredPathExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	c := Config{Listen: "127.0.0.1:0", ScrollbackBytes: 1, IdleAfterSeconds: 2, DefaultCwd: home, Tunnel: TunnelConfig{Cloudflared: "~/bin/cloudflared"}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Tunnel.Cloudflared != filepath.Join(home, "bin", "cloudflared") {
		t.Fatalf("cloudflared path %q", c.Tunnel.Cloudflared)
	}
}

func TestPushConfiguration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	for _, row := range []struct {
		name, body string
		err        string // substring; empty means valid
		want       PushConfig
	}{
		{name: "absent", want: PushConfig{Enabled: true, Subject: DefaultPushSubject, TTLSeconds: 86400, Kinds: DefaultPushKinds(), IncludeTitle: true}},
		{name: "custom", body: "enabled = false\nsubject = 'mailto:me@example.com'\nttl_seconds = 0\nkinds = ['question', 'bell']\ninclude_title = false\n",
			want: PushConfig{Subject: "mailto:me@example.com", Kinds: []string{"question", "bell"}}},
		{name: "no kinds", body: "kinds = []\n", want: PushConfig{Enabled: true, Subject: DefaultPushSubject, TTLSeconds: 86400, Kinds: []string{}, IncludeTitle: true}},
		{name: "max ttl", body: "ttl_seconds = 2419200\n", want: PushConfig{Enabled: true, Subject: DefaultPushSubject, TTLSeconds: MaxPushTTLSeconds, Kinds: DefaultPushKinds(), IncludeTitle: true}},
		{name: "heuristic waiting", body: "kinds = ['question', 'waiting']\n", err: `push.kinds: "waiting" is not supported`},
		{name: "duplicate kind", body: "kinds = ['done', 'done']\n", err: `"done" is listed twice`},
		{name: "http subject", body: "subject = 'http://example.com'\n", err: "https:// URL or a mailto:"},
		{name: "bare subject", body: "subject = 'example.com'\n", err: "https:// URL or a mailto:"},
		{name: "mailto without address", body: "subject = 'mailto:'\n", err: "https:// URL or a mailto:"},
		{name: "negative ttl", body: "ttl_seconds = -1\n", err: "push.ttl_seconds -1"},
		{name: "ttl too long", body: "ttl_seconds = 2419201\n", err: "push.ttl_seconds"},
	} {
		t.Run(row.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(row.name, " ", "-")+".toml")
			if err := os.WriteFile(path, []byte("[push]\n"+row.body), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := Load(path, true)
			if err != nil {
				t.Fatal(err)
			}
			err = c.Validate()
			if row.err != "" {
				if err == nil || !strings.Contains(err.Error(), row.err) {
					t.Fatalf("Validate() = %v, want %q", err, row.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(c.Push, row.want) {
				t.Fatalf("push = %#v, want %#v", c.Push, row.want)
			}
		})
	}
}
