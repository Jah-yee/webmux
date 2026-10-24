package codeview

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"path"
	"strings"
)

const MaxImageBytes = 20 * 1024 * 1024
const MaxDownloadBytes = 100 * 1024 * 1024

// headerBytes is how much of an image candidate is read to classify it.
const headerBytes = 4096

const svgMediaType = "image/svg+xml"

// rasterExtensions maps allowlisted extensions to the type implied by the
// name alone; it is used for tree icons only, the signature decides documents.
var rasterExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
}

func extension(p string) string { return strings.ToLower(path.Ext(p)) }

// imageCandidate reports whether p has an allowlisted raster extension.
func imageCandidate(p string) bool {
	_, ok := rasterExtensions[extension(p)]
	return ok
}

// nodeMediaType derives a possible image type from the name only (icons).
func nodeMediaType(name string) string {
	ext := extension(name)
	if ext == ".svg" {
		return svgMediaType
	}
	return rasterExtensions[ext]
}

// sniff matches the static raster signature table; "" when unrecognized.
func sniff(h []byte) string {
	switch {
	case bytes.HasPrefix(h, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(h, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(h, []byte("GIF87a")), bytes.HasPrefix(h, []byte("GIF89a")):
		return "image/gif"
	case len(h) >= 12 && string(h[:4]) == "RIFF" && string(h[8:12]) == "WEBP":
		return "image/webp"
	case avif(h):
		return "image/avif"
	case bytes.HasPrefix(h, []byte("BM")):
		return "image/bmp"
	case bytes.HasPrefix(h, []byte{0, 0, 1, 0}):
		return "image/x-icon"
	}
	return ""
}

// avif checks the leading ftyp box's major and compatible brands.
func avif(h []byte) bool {
	if len(h) < 16 || string(h[4:8]) != "ftyp" {
		return false
	}
	size := int(binary.BigEndian.Uint32(h[:4]))
	if size < 16 || size > len(h) {
		size = len(h)
	}
	brand := func(b []byte) bool { return string(b) == "avif" || string(b) == "avis" }
	if brand(h[8:12]) {
		return true
	}
	for i := 16; i+4 <= size; i += 4 {
		if brand(h[i : i+4]) {
			return true
		}
	}
	return false
}

// svg reports whether the first XML element of valid UTF-8 text is <svg>.
func svg(text string) bool {
	d := xml.NewDecoder(strings.NewReader(text))
	for {
		tok, err := d.Token()
		if err != nil {
			return false
		}
		if start, ok := tok.(xml.StartElement); ok {
			return start.Name.Local == "svg"
		}
	}
}

// imageDocument classifies an allowlisted raster candidate from its size and
// header only; it never needs the full content.
func imageDocument(root, p, source, oid string, size int64, header []byte) Document {
	d := document(root, p, source, oid, nil)
	d.Size = size
	d.Downloadable = size <= MaxDownloadBytes
	d.Lines = 0
	if size == 0 {
		d.Kind = "binary"
		d.Reason = "空文件"
		return d
	}
	mediaType := sniff(header)
	if mediaType == "" {
		d.Kind = "binary"
		d.Reason = "图片格式无法识别"
		return d
	}
	d.MediaType = mediaType
	if size > MaxImageBytes {
		d.Kind = "too-large"
		d.Reason = "超过 20 MiB 预览上限"
		return d
	}
	d.Kind = "image"
	return d
}

// errHeaderFull stops a header read once enough bytes were collected.
var errHeaderFull = errors.New("image header complete")

type headerWriter struct{ data []byte }

func (h *headerWriter) Write(p []byte) (int, error) {
	n := min(len(p), headerBytes-len(h.data))
	h.data = append(h.data, p[:n]...)
	if len(h.data) >= headerBytes {
		return n, errHeaderFull
	}
	return n, nil
}
