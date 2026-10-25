package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
	"image"
	"image/png"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
	m := session.NewManager(testSessionOptions(c))
	codes := codeview.New(m)
	server := httptest.NewServer(New(testDeps(t, Deps{Config: c, Manager: m, Workspace: testWorkspace(t, m, c.DefaultCwd), Code: codes})))
	defer server.Close()
	defer codes.Close()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	item, e := m.Create(c.DefaultCwd, "custom", []string{"/bin/sh"}, nil, "", nil)
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
	var context contract.CodeContext
	json.Unmarshal(raw, &context)
	query := func(p string) string {
		return "?" + url.Values{"contextId": {context.ID}, "nodeId": {base64.RawURLEncoding.EncodeToString([]byte(p))}}.Encode()
	}
	raw = request("GET", base+"/file"+query("clean.txt"), "", "secret", "", 200)
	var file contract.CodeFile
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
	body, _ := json.Marshal(contract.CodeCheckRequest{Context: context.ID, Snapshots: []string{file.Snapshot}})
	request("POST", base+"/check", string(body), "secret", "http://evil.example", 403)
	request("POST", base+"/check", string(body), "secret", "", 200)
	data, e := os.ReadFile(filepath.Join(c.DefaultCwd, "clean.txt"))
	if e != nil || string(data) != "unchanged code\n" {
		t.Fatal("read changed file")
	}
	other, e := m.Create(c.DefaultCwd, "custom", []string{"/bin/sh"}, nil, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	request("GET", "/api/sessions/"+other.Info().ID+"/code/snapshots/"+file.Snapshot+"/file?side=new", "", "secret", "", 400)
}

func TestCodeRawBytesHeadersAndStreaming(t *testing.T) {
	c, e := config.Load(filepath.Join(t.TempDir(), "absent"), false)
	if e != nil {
		t.Fatal(e)
	}
	c.DefaultCwd = t.TempDir()
	c.Token = "secret"
	m := session.NewManager(testSessionOptions(c))
	codes := codeview.New(m)
	server := httptest.NewServer(New(testDeps(t, Deps{Config: c, Manager: m, Workspace: testWorkspace(t, m, c.DefaultCwd), Code: codes})))
	defer server.Close()
	defer codes.Close()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	item, e := m.Create(c.DefaultCwd, "custom", []string{"/bin/sh"}, nil, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	base := "/api/sessions/" + item.Info().ID + "/code"
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var pic bytes.Buffer
	if e = png.Encode(&pic, img); e != nil {
		t.Fatal(e)
	}
	large := bytes.Repeat([]byte("PK\x03\x04streamed archive bytes\x00"), 1<<20)
	files := map[string][]byte{
		"pic.png":    pic.Bytes(),
		"图片.png":     pic.Bytes(),
		"a\x01b.png": pic.Bytes(),
		"icon.svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"bundle.zip": large,
	}
	for name, data := range files {
		if e = os.WriteFile(filepath.Join(c.DefaultCwd, name), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	do := func(method, path, token, origin string) *http.Response {
		t.Helper()
		r, _ := http.NewRequest(method, server.URL+path, nil)
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
		return res
	}
	res := do("GET", base+"/context", "secret", "")
	var ctxInfo contract.CodeContext
	json.NewDecoder(res.Body).Decode(&ctxInfo)
	res.Body.Close()
	snapshot := func(name string) string {
		t.Helper()
		q := url.Values{"contextId": {ctxInfo.ID}, "nodeId": {base64.RawURLEncoding.EncodeToString([]byte(name))}}
		res := do("GET", base+"/file?"+q.Encode(), "secret", "")
		defer res.Body.Close()
		var f contract.CodeFile
		if e := json.NewDecoder(res.Body).Decode(&f); e != nil || res.StatusCode != 200 {
			t.Fatalf("%s: %d %v", name, res.StatusCode, e)
		}
		return base + "/snapshots/" + f.Snapshot + "/raw"
	}
	check := func(res *http.Response, want []byte, contentType, disposition, filename string) {
		t.Helper()
		defer res.Body.Close()
		body, e := io.ReadAll(res.Body)
		if e != nil || res.StatusCode != 200 || !bytes.Equal(body, want) {
			t.Fatalf("status %d len %d err %v", res.StatusCode, len(body), e)
		}
		h := res.Header
		if h.Get("Content-Type") != contentType || h.Get("Content-Length") != strconv.Itoa(len(want)) || h.Get("Cache-Control") != "no-store" || h.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("headers %v", h)
		}
		kind, params, e := mime.ParseMediaType(h.Get("Content-Disposition"))
		if e != nil || kind != disposition || params["filename"] != filename {
			t.Fatalf("disposition %q", h.Get("Content-Disposition"))
		}
		if (contentType == "image/svg+xml") != (h.Get("Content-Security-Policy") == "default-src 'none'; style-src 'unsafe-inline'; sandbox") {
			t.Fatalf("csp %q", h.Get("Content-Security-Policy"))
		}
	}
	raw := snapshot("pic.png")
	for _, token := range []string{"", "wrong"} {
		res := do("GET", raw, token, "")
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 401 || bytes.Contains(body, pic.Bytes()) {
			t.Fatalf("token %q: %d", token, res.StatusCode)
		}
	}
	res = do("GET", raw, "secret", "http://evil.example")
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("origin: %d", res.StatusCode)
	}
	check(do("GET", raw, "secret", ""), pic.Bytes(), "image/png", "inline", "pic.png")
	check(do("GET", raw+"?download=1", "secret", ""), pic.Bytes(), "image/png", "attachment", "pic.png")
	check(do("GET", snapshot("图片.png"), "secret", ""), pic.Bytes(), "image/png", "inline", "图片.png")
	check(do("GET", snapshot("a\x01b.png"), "secret", ""), pic.Bytes(), "image/png", "inline", "download")
	check(do("GET", snapshot("icon.svg"), "secret", ""), files["icon.svg"], "image/svg+xml", "inline", "icon.svg")
	zip := snapshot("bundle.zip")
	head := do("HEAD", zip, "secret", "")
	body, _ := io.ReadAll(head.Body)
	head.Body.Close()
	if head.StatusCode != 200 || len(body) != 0 || head.ContentLength != int64(len(large)) || head.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("HEAD %d %d %v", head.StatusCode, len(body), head.Header)
	}
	res = do("GET", "/api/sessions/"+item.Info().ID+"/code/snapshots/missing/raw", "secret", "")
	var errBody map[string]string
	if json.NewDecoder(res.Body).Decode(&errBody); res.StatusCode != 410 || errBody["error"] == "" {
		t.Fatalf("expired: %d %v", res.StatusCode, errBody)
	}
	res.Body.Close()
	// A reader slower than the 10 s JSON timeout still receives every byte.
	slow := do("GET", zip, "secret", "")
	first := make([]byte, 1024)
	if _, e = io.ReadFull(slow.Body, first); e != nil {
		t.Fatal(e)
	}
	time.Sleep(11 * time.Second)
	rest, e := io.ReadAll(slow.Body)
	slow.Body.Close()
	if e != nil || !bytes.Equal(append(first, rest...), large) {
		t.Fatalf("slow read truncated: %d %v", len(first)+len(rest), e)
	}
	if slow.Header.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(slow.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("zip headers %v", slow.Header)
	}
	check(do("GET", zip, "secret", ""), large, "application/octet-stream", "attachment", "bundle.zip")
}

func TestContentDispositionFallback(t *testing.T) {
	for name, want := range map[string]string{
		"pic.png":     `inline; filename=pic.png`,
		"a b.png":     `inline; filename="a b.png"`,
		"tab\there":   `inline; filename=download`,
		"del\x7f.png": `inline; filename=download`,
		"\xff.png":    `inline; filename=download`,
		"图.png":       `inline; filename*=utf-8''%E5%9B%BE.png`,
	} {
		if got := contentDisposition("inline", name); got != want {
			t.Errorf("%q: %q want %q", name, got, want)
		}
	}
}
