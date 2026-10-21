package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/session"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodeAPIReadOnlyAndSessionSecurity(t *testing.T) {
	c, e := config.Load(filepath.Join(t.TempDir(), "absent"), false)
	if e != nil {
		t.Fatal(e)
	}
	c.DefaultCwd = t.TempDir()
	c.Token = "secret"
	m := session.NewManager(c.SessionOptions())
	codes := codeview.New(m)
	server := httptest.NewServer(New(c, m, testWorkspace(t, m, c.DefaultCwd), nil, codes))
	defer server.Close()
	defer codes.Close()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	item, e := m.Create(c.DefaultCwd, "custom", []string{"/bin/sh"}, nil, "")
	if e != nil {
		t.Fatal(e)
	}
	base := "/api/sessions/" + item.Info().ID + "/code"
	os.WriteFile(filepath.Join(c.DefaultCwd, "clean.txt"), []byte("unchanged code\n"), 0600)
	request := func(method, path, body, token, origin string, want int) []byte {
		t.Helper()
		r, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		res, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var raw json.RawMessage
		if e = json.NewDecoder(res.Body).Decode(&raw); e != nil {
			t.Fatal(e)
		}
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, res.StatusCode, want, raw)
		}
		return raw
	}
	request("GET", base+"/context", "", "", "", 401)
	request("GET", base+"/context", "", "secret", "http://evil.example", 403)
	raw := request("GET", base+"/context", "", "secret", "", 200)
	var context codeview.Context
	json.Unmarshal(raw, &context)
	query := func(p string) string {
		return "?" + url.Values{"contextId": {context.ID}, "nodeId": {base64.RawURLEncoding.EncodeToString([]byte(p))}}.Encode()
	}
	raw = request("GET", base+"/file"+query("clean.txt"), "", "secret", "", 200)
	var file codeview.File
	json.Unmarshal(raw, &file)
	if file.Document.Text != "unchanged code\n" {
		t.Fatal(file)
	}
	for _, path := range []string{"../other", "/absolute", ".git/config"} {
		request("GET", base+"/file"+query(path), "", "secret", "", 400)
	}
	request("GET", base+"/file"+query("missing"), "", "secret", "", 404)
	request("GET", base+"/snapshots/unknown/file?side=new", "", "secret", "", 410)
	request("GET", base+"/file?contextId=wrong&nodeId=Y2xlYW4udHh0", "", "secret", "", 409)
	request("GET", "/api/sessions/missing/code/context", "", "secret", "", 404)
	body, _ := json.Marshal(codeview.CheckRequest{Context: context.ID, Snapshots: []string{file.Snapshot}})
	request("POST", base+"/check", string(body), "secret", "http://evil.example", 403)
	request("POST", base+"/check", string(body), "secret", "", 200)
	data, e := os.ReadFile(filepath.Join(c.DefaultCwd, "clean.txt"))
	if e != nil || string(data) != "unchanged code\n" {
		t.Fatal("read changed file")
	}
	other, e := m.Create(c.DefaultCwd, "custom", []string{"/bin/sh"}, nil, "")
	if e != nil {
		t.Fatal(e)
	}
	request("GET", "/api/sessions/"+other.Info().ID+"/code/snapshots/"+file.Snapshot+"/file?side=new", "", "secret", "", 400)
}
