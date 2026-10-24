// Package statefile publishes small private files atomically: readers see
// either the previous file or the complete new one, never a partial write.
package statefile

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Perm is the mode of every file Write publishes. State files may hold
// tokens, keys or launch environments, so they are private regardless of the
// process umask.
const Perm os.FileMode = 0600

// Options describe how Write stages and publishes a file.
type Options struct {
	// TempPattern is the os.CreateTemp pattern of the staging file created
	// next to the target, e.g. ".push-*". It names leftovers after a crash.
	TempPattern string
	// Exclusive publishes with a hard link instead of a rename, so an existing
	// target is never replaced: Write then fails with an error matching
	// fs.ErrExist and leaves the target untouched.
	Exclusive bool
	// RequireDirSync reports a failed fsync of the parent directory as a
	// *DirSyncError. Without it the directory fsync is best effort.
	RequireDirSync bool
}

// DirSyncError means the new file is already visible at its path but the
// parent directory could not be synced, so the replacement may not survive a
// crash. Its message is that of the underlying error.
type DirSyncError struct{ Err error }

func (e *DirSyncError) Error() string { return e.Err.Error() }
func (e *DirSyncError) Unwrap() error { return e.Err }

// Write atomically publishes data at path as a Perm file. It stages the bytes
// in a temporary file in the same directory, fsyncs and closes it, then
// renames (or, with Exclusive, links) it into place and fsyncs the directory.
// The temporary file is removed on every path except a successful rename.
// The parent directory must already exist.
func Write(path string, data []byte, opt Options) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, opt.TempPattern)
	if err != nil {
		return err
	}
	tmp := f.Name()
	published := false
	defer func() {
		if !published || opt.Exclusive {
			_ = os.Remove(tmp)
		}
	}()
	if err = f.Chmod(Perm); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if opt.Exclusive {
		err = os.Link(tmp, path)
	} else {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		return err
	}
	published = true
	if err := syncDir(dir); err != nil && opt.RequireDirSync {
		return &DirSyncError{Err: err}
	}
	return nil
}

// WriteJSON publishes v as two-space indented JSON without a trailing newline.
func WriteJSON(path string, v any, opt Options) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return Write(path, data, opt)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	_ = d.Close()
	return err
}
