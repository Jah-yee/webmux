package statefile

import (
	"encoding/json"
	"errors"
	"github.com/rainchan/webmux/internal/jsonutil"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// Handle owns one registered file and its lifetime lock. Values belong to the
// caller; all writes still pass the same strict codec and domain validator.
type Handle[T any] struct {
	mu       sync.Mutex
	desc     Descriptor
	path     string
	lock     *os.File
	validate func(T) error
	broken   error
	// Per-handle fault injection, never a process-global hook.
	publish func(string, []byte, Options) error
	remove  func(string) error
	syncDir func(string) error
}

func Open[T any](dir string, id ID, validate func(T) error) (*Handle[T], T, error) {
	return openStore(dir, Describe(id), validate)
}

// OpenRuntime uses the caller's existing runtime directory. Only runtime owns this lifecycle.
func OpenRuntime[T any](dir string, validate func(T) error) (*Handle[T], T, error) {
	d := Describe(Runtime)
	d.Path = filepath.Base(d.Path)
	return openStore(dir, d, validate)
}
func openStore[T any](dir string, d Descriptor, validate func(T) error) (*Handle[T], T, error) {
	var zero T
	id := d.ID
	h := &Handle[T]{desc: d, path: filepath.Join(dir, d.Path), validate: validate, publish: Write, remove: os.Remove, syncDir: syncDir}
	fail := func(err error) (*Handle[T], T, error) { h.broken = err; return h, zero, err }
	if d.ID == "" {
		return fail(fault(d, dir, "open", Corrupt, nil))
	}
	if err := CheckLegacy(dir, id); err != nil {
		return fail(err)
	}
	parent := filepath.Dir(h.path)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return fail(fault(d, h.path, "mkdir", IO, err))
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return fail(fault(d, h.path, "directory", IO, err))
	}
	if err = os.Chmod(parent, 0700); err != nil {
		return fail(fault(d, h.path, "chmod", IO, err))
	}
	f, err := openPrivate(h.path+".lock", true)
	if err != nil {
		return fail(fault(d, h.path, "lock", IO, err))
	}
	if err = f.Chmod(Perm); err != nil {
		_ = f.Close()
		return fail(fault(d, h.path, "lock", IO, err))
	}
	if err = lockFile(f); err != nil {
		_ = f.Close()
		return fail(fault(d, h.path, "lock", Locked, err))
	}
	h.lock = f
	value, err := readSnapshot[T](dir, d, validate)
	if IsKind(err, Missing) {
		return h, zero, err
	}
	if err != nil {
		return fail(err)
	}
	if err = os.Chmod(h.path, Perm); err != nil {
		return fail(fault(d, h.path, "chmod", IO, err))
	}
	return h, value, nil
}
func CheckLegacy(dir string, id ID) error {
	d := Describe(id)
	if d.Legacy == "" {
		return nil
	}
	p := filepath.Join(dir, d.Legacy)
	if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
		return fault(d, p, "legacy: stop old version and verify/remove its owned entry before starting", LegacyLayout, nil)
	}
	return nil
}
func ReadSnapshot[T any](dir string, id ID, validate func(T) error) (T, error) {
	return readSnapshot(dir, Describe(id), validate)
}
func ReadRuntime[T any](dir string, validate func(T) error) (T, error) {
	d := Describe(Runtime)
	d.Path = filepath.Base(d.Path)
	return readSnapshot(dir, d, validate)
}
func readSnapshot[T any](dir string, d Descriptor, validate func(T) error) (T, error) {
	var value T
	id := d.ID
	p := filepath.Join(dir, d.Path)
	if d.ID == "" {
		return value, fault(d, p, "read", Corrupt, nil)
	}
	if err := CheckLegacy(dir, id); err != nil {
		return value, err
	}
	f, err := openPrivate(p, false)
	if err != nil {
		kind := IO
		if errors.Is(err, os.ErrNotExist) {
			if _, e := os.Lstat(p); errors.Is(e, os.ErrNotExist) {
				kind = Missing
			} else {
				kind = Corrupt
			}
		}
		return value, fault(d, p, "read", kind, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, d.MaxBytes+1))
	if err != nil {
		return value, fault(d, p, "read", IO, err)
	}
	if int64(len(data)) > d.MaxBytes {
		return value, fault(d, p, "read", TooLarge, nil)
	}
	err = decodeDocument(data, d, &value, validate)
	if err != nil {
		return value, fault(d, p, "decode", decodeKind(err), nil)
	}
	return value, nil
}

var errVersion = errors.New("unsupported version")

func decodeKind(err error) Kind {
	if errors.Is(err, errVersion) {
		return UnsupportedVersion
	}
	return Corrupt
}
func decodeDocument[T any](data []byte, d Descriptor, out *T, validate func(T) error) error {
	if err := Decode(data, out); err != nil {
		return err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	var version int
	if json.Unmarshal(object[d.VersionKey], &version) != nil || version != d.Version {
		return errVersion
	}
	if err := jsonutil.Required(data, out); err != nil {
		return err
	}
	if validate != nil {
		return validate(*out)
	}
	return nil
}
func (h *Handle[T]) Path() string {
	if h == nil {
		return ""
	}
	return h.path
}
func (h *Handle[T]) Err() error {
	if h == nil {
		return fault(Descriptor{}, "", "access", Closed, nil)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.broken
}
func (h *Handle[T]) Commit(value T) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.broken != nil {
		return h.broken
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fault(h.desc, h.path, "encode", Corrupt, nil)
	}
	if int64(len(data)) > h.desc.MaxBytes {
		return fault(h.desc, h.path, "encode", TooLarge, nil)
	}
	var checked T
	if err = decodeDocument(data, h.desc, &checked, h.validate); err != nil {
		return fault(h.desc, h.path, "validate", decodeKind(err), nil)
	}
	// Reject a newly replaced symlink/non-regular target too.
	if info, e := os.Lstat(h.path); e == nil && !info.Mode().IsRegular() {
		return fault(h.desc, h.path, "publish", Corrupt, nil)
	} else if e != nil && !errors.Is(e, os.ErrNotExist) {
		return fault(h.desc, h.path, "publish", IO, e)
	}
	err = h.publish(h.path, data, Options{TempPattern: ".state-*", RequireDirSync: true})
	return h.commitError("publish", err)
}
func (h *Handle[T]) Remove() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.broken != nil {
		return h.broken
	}
	if info, err := os.Lstat(h.path); err == nil && !info.Mode().IsRegular() {
		return fault(h.desc, h.path, "remove", Corrupt, nil)
	} else if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fault(h.desc, h.path, "remove", IO, err)
	}
	err := h.remove(h.path)
	if err != nil {
		return h.commitError("remove", err)
	}
	if err = h.syncDir(filepath.Dir(h.path)); err != nil {
		return h.commitError("remove", &DirSyncError{Err: err})
	}
	return nil
}
func (h *Handle[T]) commitError(op string, err error) error {
	if err == nil {
		return nil
	}
	var syncErr *DirSyncError
	if errors.As(err, &syncErr) {
		h.broken = fault(h.desc, h.path, op, CommitUncertain, err)
		return h.broken
	}
	return fault(h.desc, h.path, op, IO, err)
}
func (h *Handle[T]) Close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lock != nil {
		unlockFile(h.lock)
		_ = h.lock.Close()
		h.lock = nil
	}
	if h.broken == nil {
		h.broken = fault(h.desc, h.path, "close", Closed, nil)
	}
}
