package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rainchan/webmux/internal/contract"
)

func TestConfiguredViewIsReadOnlyAndRedacted(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	path := filepath.Join(root, "config.toml")
	data := fmt.Sprintf("state_dir = %q\ntoken = 'PRIVATE_LOGIN_SENTINEL'\n[tunnel]\ntoken = 'PRIVATE_TUNNEL_SENTINEL'\nhostname = 'term.example.com'\n", state)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := configShow([]string{"--config", path, "--json"}, &out, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if strings.Contains(out.String()+stderr.String(), "PRIVATE_") {
		t.Fatal("secret leaked")
	}
	var v contract.Configuration
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.View != "configured" || v.Fields["token"].Source != "toml" || v.Instance != nil {
		t.Fatalf("view: %+v", v)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("configured inspection initialized state")
	}
	out.Reset()
	stderr.Reset()
	if code := configShow([]string{"--config", path, "--json", "--live"}, &out, &stderr); code == 0 || out.Len() != 0 {
		t.Fatalf("live silently fell back: %d %s", code, out.String())
	}
	if strings.Contains(stderr.String(), "PRIVATE_") {
		t.Fatal("live error leaked secret")
	}
}
func TestConfiguredViewCorruptOverrideFailsClosed(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	file := filepath.Join(root, "tunnel-settings.json")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("state_dir = %q\n[tunnel]\nenabled = true\n", root)), 0600); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"v":1,"token":"PRIVATE_BODY_SENTINEL",bad}`)
	if err := os.WriteFile(file, original, 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := configShow([]string{"--config", path, "--json"}, &out, &stderr); code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	var v contract.Configuration
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Fields["tunnel.enabled"].Value != false {
		t.Fatal("unsafe TOML fallback")
	}
	if strings.Contains(out.String(), "PRIVATE_BODY_SENTINEL") {
		t.Fatal("leaked corrupt body")
	}
	got, _ := os.ReadFile(file)
	if !bytes.Equal(got, original) {
		t.Fatal("corrupt body changed")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 2 {
		t.Fatal("offline view created files")
	}
}

func TestLiveViewRetainsAdoptedValuesAfterDiskDrift(t *testing.T) {
	r := startServe(t, false, "PRIVATE_LIVE_SENTINEL")
	path := filepath.Join(r.dir, "config.toml")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("state_dir = %q\nlisten = '127.0.0.1:9999'\ntoken = 'PRIVATE_LIVE_SENTINEL'\nscrollback_bytes = 4096\n", r.state)), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := configShow([]string{"--config", path, "--live", "--json"}, &out, &stderr); code != 0 {
		t.Fatalf("live exit %d: %s", code, stderr.String())
	}
	var live contract.Configuration
	if err := json.Unmarshal(out.Bytes(), &live); err != nil {
		t.Fatal(err)
	}
	if live.View != "effective" || live.Instance == nil || live.Listen == "127.0.0.1:9999" || live.Fields["scrollback_bytes"].Value == float64(4096) {
		t.Fatalf("live reread disk: %+v", live)
	}
	if strings.Contains(out.String(), "PRIVATE_LIVE_SENTINEL") {
		t.Fatal("live secret leaked")
	}
	out.Reset()
	if code := configShow([]string{"--config", path, "--json"}, &out, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	var disk contract.Configuration
	if err := json.Unmarshal(out.Bytes(), &disk); err != nil {
		t.Fatal(err)
	}
	if disk.Listen != "127.0.0.1:9999" || disk.Fields["scrollback_bytes"].Value != float64(4096) {
		t.Fatal("disk view ignored disk edit")
	}
}
