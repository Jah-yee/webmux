package codeview

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pngBytes(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for x := range 3 {
		for y := range 2 {
			img.Set(x, y, color.RGBA{shade, 10, 20, 255})
		}
	}
	var out bytes.Buffer
	if e := png.Encode(&out, img); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}

func writeBytes(t *testing.T, root, p string, data []byte) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(root, p), data, 0600); e != nil {
		t.Fatal(e)
	}
}

// sparse writes header and extends the file to size without allocating it.
func sparse(t *testing.T, root, p string, header []byte, size int64) {
	t.Helper()
	writeBytes(t, root, p, header)
	if e := os.Truncate(filepath.Join(root, p), size); e != nil {
		t.Fatal(e)
	}
}

func TestSniffSignatures(t *testing.T) {
	t.Parallel()
	avifHeader := append([]byte{0, 0, 0, 0x1c}, []byte("ftypmif1\x00\x00\x00\x00mif1avifmiaf")...)
	avisCompat := append([]byte{0, 0, 0, 0x18}, []byte("ftypmsf1\x00\x00\x00\x00avis")...)
	cases := map[string]struct {
		header []byte
		want   string
	}{
		"png":         {[]byte("\x89PNG\r\n\x1a\nrest"), "image/png"},
		"jpeg":        {[]byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		"gif87":       {[]byte("GIF87a.."), "image/gif"},
		"gif89":       {[]byte("GIF89a.."), "image/gif"},
		"webp":        {[]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), "image/webp"},
		"avif major":  {[]byte("\x00\x00\x00\x18ftypavif\x00\x00\x00\x00mif1"), "image/avif"},
		"avif compat": {avifHeader, "image/avif"},
		"avis compat": {avisCompat, "image/avif"},
		"heic":        {[]byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"), ""},
		"bmp":         {[]byte("BM\x00\x00"), "image/bmp"},
		"ico":         {[]byte{0, 0, 1, 0, 1, 0}, "image/x-icon"},
		"riff wav":    {[]byte("RIFF\x10\x00\x00\x00WAVEfmt "), ""},
		"zip":         {[]byte("PK\x03\x04"), ""},
		"pdf":         {[]byte("%PDF-1.7"), ""},
		"short png":   {[]byte("\x89PN"), ""},
		"empty":       {nil, ""},
	}
	for name, c := range cases {
		if got := sniff(c.header); got != c.want {
			t.Errorf("%s: %q want %q", name, got, c.want)
		}
	}
	svgs := map[string]bool{
		`<svg xmlns="http://www.w3.org/2000/svg"/>`:                                          true,
		"<?xml version=\"1.0\"?>\n<!-- c -->\n<!DOCTYPE svg>\n<svg><script>x</script></svg>": true,
		`<s:svg xmlns:s="http://www.w3.org/2000/svg"></s:svg>`:                               true,
		`<html><svg/></html>`: false,
		`not xml at all`:      false,
		`<svg`:                false,
		``:                    false,
	}
	for text, want := range svgs {
		if svg(text) != want {
			t.Errorf("svg(%q) want %v", text, want)
		}
	}
	for name, want := range map[string]string{"A.PNG": "image/png", "b.jpeg": "image/jpeg", "c.Svg": "image/svg+xml", "d.ico": "image/x-icon", "e.bin": "", "png": "", "f.txt": ""} {
		if got := nodeMediaType(name); got != want {
			t.Errorf("node %s: %q want %q", name, got, want)
		}
	}
}

func TestImageDocumentsAndLimits(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	pic := pngBytes(t, 1)
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0}
	files := map[string][]byte{
		"a.png":        pic,
		"UPPER.PNG":    pic,
		"photo.JPG":    jpeg,
		"anim.gif":     []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"),
		"w.webp":       []byte("RIFF\x10\x00\x00\x00WEBPVP8 \x00\x00"),
		"x.avif":       []byte("\x00\x00\x00\x18ftypavif\x00\x00\x00\x00mif1"),
		"b.bmp":        []byte("BM\x1e\x00\x00\x00\x00\x00"),
		"f.ico":        {0, 0, 1, 0, 1, 0, 16, 16},
		"mismatch.png": jpeg,
		"empty.png":    {},
		"broken.png":   []byte("\x13\x37random header bytes"),
		"png.bin":      pic,
		"bundle.zip":   []byte("PK\x03\x04\x14\x00\x00\x00"),
		"doc.pdf":      []byte("%PDF-1.4\n\x00\x01binary"),
		"icon.svg":     []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`),
		"page.svg":     []byte(`<html><body/></html>`),
		"latin.svg":    []byte("<svg>\xff</svg>"),
		"plain.txt":    []byte("hello\n"),
	}
	for name, data := range files {
		writeBytes(t, root, name, data)
	}
	sparse(t, root, "limit.png", pic, MaxImageBytes)
	sparse(t, root, "huge.png", pic, MaxImageBytes+1)
	sparse(t, root, "giant.png", pic, MaxDownloadBytes+1)
	writeBytes(t, root, "big.txt", []byte(strings.Repeat("x", MaxBytes+1)))
	writeBytes(t, root, "lines.txt", []byte(strings.Repeat("x\n", MaxLines+1)))
	c, e := s.Context(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	type want struct {
		kind, media, reason string
		download            bool
	}
	cases := map[string]want{
		"a.png":        {"image", "image/png", "", true},
		"UPPER.PNG":    {"image", "image/png", "", true},
		"photo.JPG":    {"image", "image/jpeg", "", true},
		"anim.gif":     {"image", "image/gif", "", true},
		"w.webp":       {"image", "image/webp", "", true},
		"x.avif":       {"image", "image/avif", "", true},
		"b.bmp":        {"image", "image/bmp", "", true},
		"f.ico":        {"image", "image/x-icon", "", true},
		"mismatch.png": {"image", "image/jpeg", "", true},
		"empty.png":    {"binary", "", "空文件", true},
		"broken.png":   {"binary", "", "图片格式无法识别", true},
		"png.bin":      {"binary", "", "二进制内容", true},
		"bundle.zip":   {"binary", "", "二进制内容", true},
		"doc.pdf":      {"binary", "", "二进制内容", true},
		"icon.svg":     {"text", "image/svg+xml", "", true},
		"page.svg":     {"text", "", "", true},
		"latin.svg":    {"unsupported", "", "不支持的文本编码", true},
		"plain.txt":    {"text", "", "", true},
		"limit.png":    {"image", "image/png", "", true},
		"huge.png":     {"too-large", "image/png", "超过 20 MiB 预览上限", true},
		"giant.png":    {"too-large", "image/png", "超过 20 MiB 预览上限", false},
		"big.txt":      {"too-large", "", "超过 2 MiB", true},
		"lines.txt":    {"too-large", "", "超过 20000 行", true},
	}
	for name, w := range cases {
		f, e := s.File(context.Background(), id, c.ID, nodeID(name))
		if e != nil {
			t.Fatalf("%s: %v", name, e)
		}
		d := f.Document
		info, _ := os.Stat(filepath.Join(root, name))
		if d.Kind != w.kind || d.MediaType != w.media || d.Reason != w.reason || d.Downloadable != w.download || d.Size != info.Size() {
			t.Errorf("%s: %+v", name, d)
		}
		if d.Kind == "image" && (d.Text != "" || d.Lines != 0 || !d.Referenceable) {
			t.Errorf("%s: image carried text %+v", name, d)
		}
		if name == "icon.svg" && d.Text != string(files[name]) {
			t.Errorf("svg text lost")
		}
	}
	page, e := s.Tree(context.Background(), id, c.ID, "", false, "")
	if e != nil {
		t.Fatal(e)
	}
	for _, n := range page.Children {
		if n.MediaType != nodeMediaType(n.Name) {
			t.Errorf("node %s media %q", n.Name, n.MediaType)
		}
	}

	f, e := s.File(context.Background(), id, c.ID, nodeID("a.png"))
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Check(context.Background(), id, CheckRequest{Context: c.ID, Snapshots: []string{f.Snapshot}})
	if e != nil || r.Snapshots[f.Snapshot] != "unchanged" {
		t.Fatal("stable image reported changed", r, e)
	}
	// Same size and header, only the tail rewritten: identity (mtime) changes.
	rewritten := append([]byte(nil), pic...)
	rewritten[len(rewritten)-1] ^= 0xFF
	writeBytes(t, root, "a.png", rewritten)
	later := time.Now().Add(time.Second)
	os.Chtimes(filepath.Join(root, "a.png"), later, later)
	r, e = s.Check(context.Background(), id, CheckRequest{Context: c.ID, Snapshots: []string{f.Snapshot}})
	if e != nil || r.Snapshots[f.Snapshot] != "updated" {
		t.Fatal("image rewrite not detected", r, e)
	}
}

func TestImageDiffClassification(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	gitTest(t, root, "init", "-q")
	writeBytes(t, root, "work.png", pngBytes(t, 1))
	writeBytes(t, root, "stage.png", pngBytes(t, 2))
	writeBytes(t, root, "gone.png", pngBytes(t, 3))
	writeBytes(t, root, "swap.png", pngBytes(t, 4))
	write(t, root, "icon.svg", "<svg>\n<rect/>\n</svg>\n")
	write(t, root, "broken.svg", "<svg>\n<rect/>\n</svg>\n")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "images")
	next := pngBytes(t, 9)
	writeBytes(t, root, "work.png", next)
	writeBytes(t, root, "stage.png", pngBytes(t, 8))
	gitTest(t, root, "add", "stage.png")
	writeBytes(t, root, "added.png", pngBytes(t, 7))
	gitTest(t, root, "add", "added.png")
	os.Remove(filepath.Join(root, "gone.png"))
	write(t, root, "swap.png", "now text\n")
	write(t, root, "icon.svg", "<svg>\n<circle/>\n</svg>\n")
	write(t, root, "broken.svg", "<g>\n<rect/>\n</g>\n")
	c, e := s.Context(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	changes, e := s.Changes(context.Background(), id, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	diffs := map[string]Diff{}
	for _, change := range changes.Entries {
		d, e := s.Diff(context.Background(), id, c.ID, change.ID)
		if e != nil {
			t.Fatal(change, e)
		}
		p := change.NewPath
		if p == "" {
			p = change.OldPath
		}
		diffs[change.Group+":"+p] = d
	}
	check := func(key, kind, reason, oldSource, newSource string) Diff {
		t.Helper()
		d, ok := diffs[key]
		if !ok {
			t.Fatalf("missing %s in %v", key, changes.Entries)
		}
		if d.Kind != kind || d.Reason != reason || d.Old.Source != oldSource || d.New.Source != newSource {
			t.Fatalf("%s: kind %s reason %q old %s new %s", key, d.Kind, d.Reason, d.Old.Source, d.New.Source)
		}
		return d
	}
	work := check("unstaged:work.png", "image", "", "index", "worktree")
	if work.Old.MediaType != "image/png" || work.New.Size != int64(len(next)) || work.Old.Version == work.New.Version {
		t.Fatalf("unstaged image sides %+v", work)
	}
	check("staged:stage.png", "image", "", "head", "index")
	check("staged:added.png", "image", "", "empty", "index")
	check("unstaged:gone.png", "image", "", "index", "empty")
	check("unstaged:swap.png", "metadata", "图片与非图片内容不能对比", "index", "worktree")
	icon := check("unstaged:icon.svg", "text", "", "index", "worktree")
	if icon.New.MediaType != "image/svg+xml" || icon.New.Text == "" {
		t.Fatalf("svg diff %+v", icon)
	}
	check("unstaged:broken.svg", "text", "", "index", "worktree")
	r, e := s.Check(context.Background(), id, CheckRequest{Context: c.ID, Snapshots: []string{work.Snapshot}})
	if e != nil || r.Snapshots[work.Snapshot] != "unchanged" {
		t.Fatal("stable image diff changed", r, e)
	}
	writeBytes(t, root, "work.png", pngBytes(t, 200))
	later := time.Now().Add(time.Second)
	os.Chtimes(filepath.Join(root, "work.png"), later, later)
	r, e = s.Check(context.Background(), id, CheckRequest{Context: c.ID, Snapshots: []string{work.Snapshot}})
	if e != nil || r.Snapshots[work.Snapshot] != "updated" {
		t.Fatal("worktree image rewrite not detected", r, e)
	}
}
