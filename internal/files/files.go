// Package files owns the server's direct filesystem requests: directory
// completion for the session picker, directory creation, and storing
// uploaded files for a session to read.
package files

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/rainchan/webmux/internal/pathutil"
)

// Error kinds; the controller maps them to HTTP statuses.
var (
	ErrInvalid  = errors.New("invalid file request")
	ErrTooLarge = errors.New("file too large")
	// ErrRead marks a failure reading the upload source; the source's own
	// error stays in the chain.
	ErrRead     = errors.New("upload source read failed")
	ErrInternal = errors.New("file storage failed")
)

// Error keeps the message clients see and exposes its kind, and any cause,
// to errors.Is and errors.As.
type Error struct {
	kind    error
	cause   error
	message string
}

func (e *Error) Error() string { return e.message }
func (e *Error) Unwrap() []error {
	if e.cause == nil {
		return []error{e.kind}
	}
	return []error{e.kind, e.cause}
}

// fail wraps cause, whose text is the message.
func fail(kind, cause error) error { return &Error{kind, cause, cause.Error()} }

// failf formats the message and wraps a %w cause like fmt.Errorf.
func failf(kind error, format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	return &Error{kind, errors.Unwrap(err), err.Error()}
}

type Service struct {
	defaultCwd string
}

// New serves completions relative to defaultCwd when no prefix is given.
func New(defaultCwd string) *Service { return &Service{defaultCwd: defaultCwd} }

// Complete lists the directories, symlinks to directories included, whose
// path starts with prefix. A prefix ending in "/" (or "~") lists that
// directory. Visible names sort before hidden ones.
func (s *Service) Complete(prefix string) ([]string, error) {
	if prefix == "" {
		prefix = s.defaultCwd + string(os.PathSeparator)
	}
	expanded, err := pathutil.Expand(prefix)
	if err != nil {
		return nil, fail(ErrInvalid, err)
	}
	dir, base := filepath.Split(expanded)
	if strings.HasSuffix(prefix, "/") || prefix == "~" {
		dir = expanded
		base = ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fail(ErrInvalid, err)
	}
	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i].Name(), entries[j].Name()
		leftHidden, rightHidden := strings.HasPrefix(left, "."), strings.HasPrefix(right, ".")
		if leftHidden != rightHidden {
			return !leftHidden
		}
		return left < right
	})
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), base) {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			stat, err := os.Stat(full)
			if err != nil {
				continue
			}
			isDir = stat.IsDir()
		}
		if isDir {
			out = append(out, full)
		}
	}
	return out, nil
}

// Mkdir creates path and its parents.
func (s *Service) Mkdir(path string) error {
	if path == "" {
		return fail(ErrInvalid, errors.New("path required"))
	}
	p, err := pathutil.Expand(path)
	if err == nil {
		err = os.MkdirAll(p, 0755)
	}
	if err != nil {
		return fail(ErrInvalid, err)
	}
	return nil
}

// Upload is a stored file. Discard removes it until the caller keeps it.
type Upload struct {
	Path string
	dir  string
}

// Discard deletes the upload's private directory.
func (u *Upload) Discard() { _ = os.RemoveAll(u.dir) }

// uploadFilename reduces a client-supplied filename to its base name and
// refuses control characters and names that do not name a file.
func uploadFilename(name string) (string, error) {
	for _, ch := range name {
		if unicode.IsControl(ch) {
			return "", fail(ErrInvalid, errors.New("upload filename contains an unsafe control character"))
		}
	}
	// Treat both slash styles as separators, including filenames supplied by
	// Windows clients. Inspect the original name before discarding directories.
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return "", fail(ErrInvalid, errors.New("upload filename must name a file"))
	}
	return name, nil
}

// StoreUpload copies at most limit bytes of src into a new file named after
// filename, inside a private (0700) temporary directory, with mode 0600. On
// any error nothing is left behind.
func (s *Service) StoreUpload(filename string, src io.Reader, limit int64) (*Upload, error) {
	name, err := uploadFilename(filename)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "webmux-upload-")
	if err != nil {
		return nil, failf(ErrInternal, "create upload temporary directory: %w", err)
	}
	upload := &Upload{dir: dir}
	complete := false
	defer func() {
		if !complete {
			upload.Discard()
		}
	}()
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, failf(ErrInternal, "set upload directory permissions: %w", err)
	}
	absoluteDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, failf(ErrInternal, "resolve upload temporary directory: %w", err)
	}
	upload.Path = filepath.Join(absoluteDir, name)
	file, err := os.OpenFile(upload.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, failf(ErrInternal, "create uploaded file: %w", err)
	}
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return nil, failf(ErrInternal, "set uploaded file permissions: %w", err)
	}
	written, err := copyBounded(file, src, limit)
	if err != nil {
		var pathError *os.PathError
		if errors.As(err, &pathError) && pathError.Op == "write" {
			return nil, failf(ErrInternal, "write uploaded file: %w", err)
		}
		return nil, failf(ErrRead, "read uploaded file: %w", err)
	}
	if written > limit {
		return nil, fail(ErrTooLarge, fmt.Errorf("uploaded file exceeds %d MiB", limit>>20))
	}
	if err := file.Close(); err != nil {
		return nil, failf(ErrInternal, "close uploaded file: %w", err)
	}
	complete = true
	return upload, nil
}

// Read at most one byte beyond the file limit to distinguish a file exactly at
// the limit from an oversized file without buffering the upload in memory.
func copyBounded(dst io.Writer, src io.Reader, limit int64) (int64, error) {
	return io.Copy(dst, io.LimitReader(src, limit+1))
}
