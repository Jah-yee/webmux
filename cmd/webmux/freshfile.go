package main

import (
	"errors"
	"os"
	"path/filepath"
)

// createFresh creates path as a new 0600 file opened with
// O_WRONLY|O_CREATE|O_EXCL plus flag. A missing parent directory is created
// as 0700; an existing one keeps its mode. With a non-empty asideSuffix an
// existing entry at path is first renamed to path+asideSuffix, replacing an
// older one; with an empty suffix an existing entry makes the open fail with
// an error matching os.ErrExist.
func createFresh(path, asideSuffix string, flag int) (*os.File, error) {
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	if asideSuffix != "" {
		if _, err := os.Lstat(path); err == nil {
			if err := os.Rename(path, path+asideSuffix); err != nil {
				return nil, err
			}
		}
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|flag, 0600)
}
