package statefile

import (
	"errors"
	"os"
)

// Windows clients only use the atomic publisher; server stores run on Unix.
func openPrivate(path string, write bool) (*os.File, error) {
	if write {
		return nil, errors.New("server stores require Linux or macOS")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	return os.Open(path)
}
func lockFile(*os.File) error { return errors.New("server stores require Linux or macOS") }

func unlockFile(_ *os.File) {}
