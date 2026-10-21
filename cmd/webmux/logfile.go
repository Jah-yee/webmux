package main

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
)

// redirectOutput points fd 1 and 2 at path so logs and runtime panics
// survive the launcher closing its pipes. An existing file is kept as
// path+".1", replacing an older one.
func redirectOutput(path string) error {
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(path); err == nil {
		if err := os.Rename(path, path+".1"); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	for _, fd := range []int{1, 2} {
		if err := dupTo(int(f.Fd()), fd); err != nil {
			return err
		}
	}
	// Rebuild the default handler on the redirected stderr.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	return nil
}
